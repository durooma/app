package eval

import (
	"context"
	"errors"
	"math"
	"reflect"
	"testing"

	"durooma/internal/ai"
)

type classifyFunc func(context.Context, []ai.Item, []ai.CategoryDef) ([]string, error)

func (f classifyFunc) Name() string { return "test" }
func (f classifyFunc) Classify(ctx context.Context, items []ai.Item, cats []ai.CategoryDef) ([]string, error) {
	return f(ctx, items, cats)
}

func fixture() (Config, Dataset) {
	c := Config{
		Retry:   RetryPolicy{MaxAttempts: 1},
		Models:  []Model{{Name: "one", Provider: "gemini", Model: "test", APIKeyEnv: "TEST_KEY"}},
		Prompts: []Prompt{{Name: "baseline"}}, BatchSizes: []int{2}, Repeats: 1, Seed: 42,
		Timeout: "1s", RequestInterval: "0s", Concurrency: 1,
	}
	d := Dataset{Name: "test", Categories: []Category{{Name: "Dining"}, {Name: "Groceries"}}, Examples: []Example{
		{ID: "a", Description: "restaurant", Amount: -10, Expected: "Dining"},
		{ID: "b", Description: "supermarket", Amount: -20, Expected: "Groceries"},
		{ID: "c", Description: "cafe", Amount: -5, Expected: "Dining"},
	}}
	return c, d
}

func TestMatrixUsesSameOrderAndKeepsRemainder(t *testing.T) {
	c, d := fixture()
	c.Models = append(c.Models, Model{Name: "two", Provider: "gemini", Model: "test2", APIKeyEnv: "TEST_KEY"})
	c.Prompts = append(c.Prompts, Prompt{Name: "other", Template: "{{.Transactions}} {{.Categories}}"})
	c.BatchSizes, c.Repeats = []int{1, 2}, 2
	calls := 0
	factory := func(Model, Prompt) (ai.Provider, error) {
		return classifyFunc(func(_ context.Context, items []ai.Item, _ []ai.CategoryDef) ([]string, error) {
			calls++
			names := make([]string, len(items))
			for i, item := range items {
				names[i] = " dining "
				if item.Desc == "supermarket" {
					names[i] = "GROCERIES"
				}
			}
			return names, nil
		}), nil
	}
	checkpoints := 0
	r, err := Run(context.Background(), c, d, "hash", factory, func(Report) error { checkpoints++; return nil })
	if err != nil {
		t.Fatal(err)
	}
	if calls != RequestCount(c, len(d.Examples)) || len(r.Results) != 8 || checkpoints != 8 {
		t.Fatalf("calls=%d results=%d checkpoints=%d", calls, len(r.Results), checkpoints)
	}
	var baseline []string
	for i, result := range r.Results {
		if !result.Complete || result.Metrics.Accuracy != 1 || result.Metrics.MacroF1 != 1 || result.Metrics.Evaluated != 6 {
			t.Fatalf("bad result: %+v", result)
		}
		var order []string
		for _, p := range result.Predictions {
			order = append(order, p.ID)
		}
		if i == 0 {
			baseline = order
		} else if !reflect.DeepEqual(order, baseline) {
			t.Fatalf("variant %d used a different input order", i)
		}
		if result.BatchSize == 2 && result.Batches[1].Size != 1 {
			t.Fatal("partial final batch missing")
		}
	}
}

func TestFailuresCountAgainstAccuracy(t *testing.T) {
	for _, tc := range []struct {
		name           string
		fn             classifyFunc
		failedRequests int
	}{
		{"API error", func(context.Context, []ai.Item, []ai.CategoryDef) ([]string, error) { return nil, errors.New("quota") }, 2},
		{"wrong count", func(context.Context, []ai.Item, []ai.CategoryDef) ([]string, error) { return []string{}, nil }, 2},
		{"invalid names", func(_ context.Context, items []ai.Item, _ []ai.CategoryDef) ([]string, error) {
			out := make([]string, len(items))
			for i := range out {
				out[i] = "invented"
			}
			return out, nil
		}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, d := fixture()
			r, err := Run(context.Background(), c, d, "", func(Model, Prompt) (ai.Provider, error) { return tc.fn, nil }, nil)
			if err != nil {
				t.Fatal(err)
			}
			m := r.Results[0].Metrics
			if m.Evaluated != 3 || m.Errors != 3 || m.Accuracy != 0 || m.MacroF1 != 0 || m.FailedRequests != tc.failedRequests {
				t.Fatalf("failures were hidden: %+v", m)
			}
		})
	}
}

func TestScoringIncludesFalsePositivesAndFailureBucket(t *testing.T) {
	m := score([]Prediction{
		{Expected: "A", Predicted: "A", Correct: true},
		{Expected: "A", Predicted: "B"},
		{Expected: "B", Predicted: "B", Correct: true},
		{Expected: "B", Error: "request failed"},
	}, []Batch{{LatencyMS: 10}, {LatencyMS: 30, Error: "failed"}}, []Category{{Name: "A"}, {Name: "B"}, {Name: "Absent"}})
	if m.Accuracy != .5 || m.ErrorRate != .25 || math.Abs(m.MacroF1-(2.0/3+.5)/2) > 1e-12 {
		t.Fatalf("incorrect scoring: %+v", m)
	}
	if m.Confusion["B"][""] != 1 || m.P95LatencyMS != 30 || m.MeanLatencyMS != 20 || m.MSPerTransaction != 10 {
		t.Fatalf("incorrect confusion/latency: %+v", m)
	}
}

func TestCancellationCheckpointsPartialVariant(t *testing.T) {
	c, d := fixture()
	c.RequestInterval = "1h"
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls, checkpoints := 0, 0
	factory := func(Model, Prompt) (ai.Provider, error) {
		return classifyFunc(func(context.Context, []ai.Item, []ai.CategoryDef) ([]string, error) {
			calls++
			cancel()
			return nil, context.Canceled
		}), nil
	}
	r, err := Run(ctx, c, d, "", factory, func(Report) error { checkpoints++; return nil })
	if !errors.Is(err, context.Canceled) || calls != 1 || checkpoints != 1 || r.Results[0].Complete || r.Results[0].Metrics.Excluded != 2 {
		t.Fatalf("err=%v calls=%d checkpoints=%d report=%+v", err, calls, checkpoints, r)
	}
}

func TestPreflightFailureMakesNoRequests(t *testing.T) {
	c, d := fixture()
	c.Models = append(c.Models, Model{Name: "two", Provider: "gemini", Model: "test2", APIKeyEnv: "MISSING"})
	calls := 0
	_, err := Run(context.Background(), c, d, "", func(m Model, _ Prompt) (ai.Provider, error) {
		if m.Name == "two" {
			return nil, errors.New("missing key")
		}
		return classifyFunc(func(context.Context, []ai.Item, []ai.CategoryDef) ([]string, error) { calls++; return nil, nil }), nil
	}, nil)
	if err == nil || calls != 0 {
		t.Fatalf("err=%v calls=%d", err, calls)
	}
}

func TestRequestTimeoutContinuesAndCheckpointFailureStops(t *testing.T) {
	c, d := fixture()
	c.Timeout = "1ms"
	c.BatchSizes = []int{2, 3}
	calls := 0
	saveErr := errors.New("disk full")
	r, err := Run(context.Background(), c, d, "", func(Model, Prompt) (ai.Provider, error) {
		return classifyFunc(func(ctx context.Context, _ []ai.Item, _ []ai.CategoryDef) ([]string, error) {
			calls++
			<-ctx.Done()
			return nil, ctx.Err()
		}), nil
	}, func(Report) error { return saveErr })
	if !errors.Is(err, saveErr) || calls < 2 || calls > 3 || len(r.Results) < 1 || !r.Results[0].Complete || r.Results[0].Metrics.Excluded != 3 {
		t.Fatalf("err=%v calls=%d report=%+v", err, calls, r)
	}
}
