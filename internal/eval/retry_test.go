package eval

import (
	"context"
	"encoding/csv"
	"errors"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"durooma/internal/ai"
)

func fastRetry() RetryPolicy {
	return RetryPolicy{MaxAttempts: 3, InitialBackoff: "1ms", MaxBackoff: "4ms"}
}

func TestRetryRecoversWithoutPenalizingScores(t *testing.T) {
	c, d := fixture()
	c.Retry = fastRetry()
	d.Examples = d.Examples[:1]
	calls := 0
	r, err := Run(context.Background(), c, d, "", func(Model, Prompt) (ai.Provider, error) {
		return classifyFunc(func(context.Context, []ai.Item, []ai.CategoryDef) ([]string, error) {
			calls++
			switch calls {
			case 1:
				return nil, &ai.NetworkError{Kind: "connection reset"}
			case 2:
				return nil, &ai.APIError{Status: 429}
			default:
				return []string{"Dining"}, nil
			}
		}), nil
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	v := r.Results[0]
	if calls != 3 || v.Metrics.Accuracy != 1 || v.Metrics.Errors != 0 || v.Metrics.Excluded != 0 || v.Metrics.Coverage != 1 || v.Requests.Retries != 2 || v.Requests.RecoveredBatches != 1 || v.Requests.TransientFailures != 2 {
		t.Fatalf("calls=%d result=%+v", calls, v)
	}
	if len(v.Batches[0].Attempts) != 3 || v.Batches[0].Attempts[1].Status != 429 || v.Batches[0].Error != "" {
		t.Fatalf("lost retry history: %+v", v.Batches)
	}
}

func TestExhaustedInfrastructureIsExcludedFromAllScores(t *testing.T) {
	c, d := fixture()
	c.Retry = fastRetry()
	c.BatchSizes = []int{1}
	for i := range d.Examples {
		d.Examples[i].Source = "personal"
		d.Examples[i].Confidence = "high"
		d.Examples[i].Expected = "Dining"
	}
	calls := 0
	r, err := Run(context.Background(), c, d, "", func(Model, Prompt) (ai.Provider, error) {
		return classifyFunc(func(_ context.Context, items []ai.Item, _ []ai.CategoryDef) ([]string, error) {
			calls++
			switch items[0].Desc {
			case "restaurant":
				return nil, &ai.APIError{Status: 429}
			case "supermarket":
				return []string{"invented"}, nil
			default:
				return []string{"Dining"}, nil
			}
		}), nil
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	v := r.Results[0]
	m := v.Metrics
	if calls != 5 || m.Attempted != 3 || m.Evaluated != 2 || m.Excluded != 1 || m.Errors != 1 || m.Accuracy != .5 || math.Abs(m.MacroF1-2.0/3) > 1e-9 || m.Coverage != 2.0/3 || !m.ScoreAvailable || v.Requests.ExhaustedBatches != 1 {
		t.Fatalf("calls=%d result=%+v", calls, v)
	}
	for _, slice := range []Metrics{v.Sources["personal"], v.ConfidenceScores["high"]} {
		if slice.Evaluated != 2 || slice.Excluded != 1 || slice.Accuracy != .5 {
			t.Fatalf("bad slice: %+v", slice)
		}
	}
	if m.Confusion["Dining"][""] != 1 {
		t.Fatal("infrastructure entered confusion matrix")
	}
	dir := t.TempDir()
	if err := WriteReport(dir, r); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"summary.csv", "slices.csv", "excluded.csv", "request_errors.csv"} {
		file, err := os.Open(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		records, err := csv.NewReader(file).ReadAll()
		file.Close()
		if err != nil {
			t.Fatal(err)
		}
		if len(records) < 2 {
			t.Fatalf("empty %s", name)
		}
	}
}

func TestTransportFailuresAndModelFailuresRemainDistinct(t *testing.T) {
	for _, tc := range []struct {
		name                                   string
		failure                                error
		names                                  []string
		wantAttempts, wantExcluded, wantErrors int
	}{
		{"network", &ai.NetworkError{Kind: "connection reset"}, nil, 3, 1, 0},
		{"server", &ai.APIError{Status: 503}, nil, 3, 1, 0},
		{"request timeout", context.DeadlineExceeded, nil, 3, 1, 0},
		{"unauthorized", &ai.APIError{Status: 401}, nil, 1, 1, 0},
		{"malformed model JSON", errors.New("model output not JSON"), nil, 1, 0, 1},
		{"wrong count", nil, []string{}, 1, 0, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, d := fixture()
			c.Retry = fastRetry()
			d.Examples = d.Examples[:1]
			calls := 0
			r, err := Run(context.Background(), c, d, "", func(Model, Prompt) (ai.Provider, error) {
				return classifyFunc(func(context.Context, []ai.Item, []ai.CategoryDef) ([]string, error) {
					calls++
					return tc.names, tc.failure
				}), nil
			}, nil)
			if err != nil {
				t.Fatal(err)
			}
			m := r.Results[0].Metrics
			if calls != tc.wantAttempts || m.Excluded != tc.wantExcluded || m.Errors != tc.wantErrors || m.Evaluated != 1-tc.wantExcluded {
				t.Fatalf("calls=%d metrics=%+v", calls, m)
			}
			if tc.wantExcluded == 1 && (m.ScoreAvailable || len(m.Confusion) != 0 || scoreCell(m.Accuracy, m) != "") {
				t.Fatal("zero coverage reported as a score")
			}
		})
	}
}

func TestRetryAfterPacingAndCancellation(t *testing.T) {
	t.Run("retry after", func(t *testing.T) {
		policy := fastRetry()
		gate := &requestGate{interval: 5 * time.Millisecond}
		var starts []time.Time
		p := classifyFunc(func(context.Context, []ai.Item, []ai.CategoryDef) ([]string, error) {
			starts = append(starts, time.Now())
			if len(starts) == 1 {
				return nil, &ai.APIError{Status: 429, RetryAfter: 25 * time.Millisecond}
			}
			return []string{"Dining"}, nil
		})
		_, b, err := classifyWithRetry(context.Background(), p, []ai.Item{{Desc: "test"}}, nil, time.Second, policy, gate)
		if err != nil || len(b.Attempts) != 2 || starts[1].Sub(starts[0]) < 25*time.Millisecond {
			t.Fatalf("b=%+v err=%v starts=%v", b, err, starts)
		}
	})
	t.Run("all starts paced", func(t *testing.T) {
		gate := &requestGate{interval: 12 * time.Millisecond}
		var starts []time.Time
		p := classifyFunc(func(context.Context, []ai.Item, []ai.CategoryDef) ([]string, error) {
			starts = append(starts, time.Now())
			return nil, &ai.NetworkError{Kind: "network"}
		})
		classifyWithRetry(context.Background(), p, []ai.Item{{Desc: "test"}}, nil, time.Second, fastRetry(), gate)
		for i := 1; i < len(starts); i++ {
			if starts[i].Sub(starts[i-1]) < 11*time.Millisecond {
				t.Fatal("retry bypassed global pacing")
			}
		}
	})
	t.Run("cancel backoff", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		invoked := make(chan struct{})
		finished := make(chan Batch)
		p := classifyFunc(func(context.Context, []ai.Item, []ai.CategoryDef) ([]string, error) {
			close(invoked)
			return nil, &ai.APIError{Status: 429, RetryAfter: time.Hour}
		})
		go func() {
			_, b, _ := classifyWithRetry(ctx, p, []ai.Item{{Desc: "test"}}, nil, time.Second, fastRetry(), &requestGate{})
			finished <- b
		}()
		<-invoked
		cancel()
		select {
		case b := <-finished:
			if len(b.Attempts) != 1 || !b.Excluded || b.RetryExhausted {
				t.Fatalf("bad cancellation: %+v", b)
			}
		case <-time.After(time.Second):
			t.Fatal("cancel did not interrupt backoff")
		}
	})
	t.Run("shared cooldown covers new jobs", func(t *testing.T) {
		gate := &requestGate{}
		began := time.Now()
		gate.pause(25 * time.Millisecond)
		if err := gate.enter(context.Background(), time.Time{}); err != nil {
			t.Fatal(err)
		}
		if time.Since(began) < 25*time.Millisecond {
			t.Fatal("new batch bypassed cooldown")
		}
	})
}

func TestRetryPolicyValidationAndBackoffBounds(t *testing.T) {
	for _, p := range []RetryPolicy{{MaxAttempts: -1}, {MaxAttempts: 21}, {InitialBackoff: "0s"}, {InitialBackoff: "2s", MaxBackoff: "1s"}, {MaxBackoff: "2h"}} {
		if p.validate() == nil {
			t.Fatalf("invalid retry accepted: %+v", p)
		}
	}
	p := RetryPolicy{MaxAttempts: 5, InitialBackoff: "2s", MaxBackoff: "8s"}
	for _, a := range []int{1, 2, 3, 8} {
		d := p.delay(a, 0)
		if d < time.Second || d > 8*time.Second {
			t.Fatal(d)
		}
	}
	if p.delay(5, time.Minute) != time.Minute {
		t.Fatal("Retry-After was capped")
	}
}
