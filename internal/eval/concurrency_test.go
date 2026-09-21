package eval

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"durooma/internal/ai"
)

func TestGlobalWorkerLimitAndStableScoring(t *testing.T) {
	c, d := fixture()
	c.Concurrency = 4
	c.BatchSizes = []int{1, 2}
	c.Repeats = 2
	c.Models = append(c.Models, Model{Name: "two", Provider: "openrouter", Model: "test", APIKeyEnv: "KEY"})
	d.Examples = nil
	for i := 0; i < 24; i++ {
		d.Examples = append(d.Examples, Example{ID: fmt.Sprint(i), Description: fmt.Sprint(i), Expected: "Dining", Source: "personal", Confidence: "high"})
	}
	var active, peak atomic.Int32
	gate := make(chan struct{})
	factory := func(Model, Prompt) (ai.Provider, error) {
		return classifyFunc(func(ctx context.Context, items []ai.Item, _ []ai.CategoryDef) ([]string, error) {
			n := active.Add(1)
			defer active.Add(-1)
			for old := peak.Load(); n > old; old = peak.Load() {
				if peak.CompareAndSwap(old, n) {
					break
				}
			}
			if n == 4 {
				select {
				case <-gate:
				default:
					close(gate)
				}
			}
			select {
			case <-gate:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
			// Deliberately finish some later batches first.
			if items[0].Desc == "7" {
				time.Sleep(time.Millisecond)
			}
			out := make([]string, len(items))
			for i := range out {
				out[i] = "Dining"
			}
			return out, nil
		}), nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	r, err := Run(ctx, c, d, "", factory, nil)
	if err != nil || peak.Load() != 4 || active.Load() != 0 || len(r.Results) != 4 {
		t.Fatalf("err=%v peak=%d active=%d results=%d", err, peak.Load(), active.Load(), len(r.Results))
	}
	var order []string
	for _, v := range r.Results {
		if !v.Complete || v.Metrics.Accuracy != 1 || v.Sources["personal"].Evaluated != 48 || v.ConfidenceScores["high"].Accuracy != 1 {
			t.Fatalf("bad result: %+v", v)
		}
		current := []string{}
		for _, p := range v.Predictions {
			current = append(current, p.ID)
		}
		if order == nil {
			order = current
		} else {
			for i := range order {
				if order[i] != current[i] {
					t.Fatal("nondeterministic prediction ordering")
				}
			}
		}
	}
	if r.WallSeconds <= 0 || r.TransactionsPerSecond <= 0 {
		t.Fatal("missing wall-clock throughput")
	}
}
func TestConcurrentCancellationDrainsAttemptedWork(t *testing.T) {
	c, d := fixture()
	c.Concurrency = 3
	c.BatchSizes = []int{1}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var started atomic.Int32
	r, err := Run(ctx, c, d, "", func(Model, Prompt) (ai.Provider, error) {
		return classifyFunc(func(ctx context.Context, _ []ai.Item, _ []ai.CategoryDef) ([]string, error) {
			if started.Add(1) == 3 {
				cancel()
			}
			<-ctx.Done()
			return nil, ctx.Err()
		}), nil
	}, nil)
	if !errors.Is(err, context.Canceled) || len(r.Results) != 1 || r.Results[0].Complete || r.Results[0].Metrics.Excluded != 3 {
		t.Fatalf("err=%v report=%+v", err, r)
	}
}
