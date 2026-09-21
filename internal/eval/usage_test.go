package eval

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"durooma/internal/ai"
)

func ptr[T any](v T) *T { return &v }

func testUsage() *ai.Usage {
	return &ai.Usage{InputTokens: ptr(int64(100)), OutputTokens: ptr(int64(40)), TotalTokens: ptr(int64(140)), ReasoningTokens: ptr(int64(30)), CachedTokens: ptr(int64(20)), CostUSD: ptr(.001)}
}

type meteredFunc func(context.Context, []ai.Item, []ai.CategoryDef) ([]string, *ai.Usage, error)

func (f meteredFunc) Name() string { return "test-metered" }
func (f meteredFunc) Classify(context.Context, []ai.Item, []ai.CategoryDef) ([]string, error) {
	panic("usage path must be used")
}
func (f meteredFunc) ClassifyWithUsage(ctx context.Context, items []ai.Item, cats []ai.CategoryDef) ([]string, *ai.Usage, error) {
	return f(ctx, items, cats)
}

func TestRunAccountsForRetriesInvalidOutputAndExcludedBatches(t *testing.T) {
	c, d := fixture()
	c.BatchSizes = []int{1}
	c.Repeats = 2
	c.Retry = RetryPolicy{MaxAttempts: 2, InitialBackoff: "1ns", MaxBackoff: "1ns"}
	calls := 0
	factory := func(Model, Prompt) (ai.Provider, error) {
		return meteredFunc(func(context.Context, []ai.Item, []ai.CategoryDef) ([]string, *ai.Usage, error) {
			calls++
			switch calls {
			case 1:
				return nil, testUsage(), &ai.APIError{Status: 503} // billed retry
			case 2:
				return []string{"Dining"}, testUsage(), nil
			case 3:
				return nil, testUsage(), errors.New("malformed model output")
			case 4, 5:
				return nil, testUsage(), &ai.APIError{Status: 503} // exhausted but counted in cost
			default:
				return []string{"Dining"}, nil, nil // missing usage is not free
			}
		}), nil
	}
	r, err := Run(context.Background(), c, d, "hash", factory, nil)
	if err != nil {
		t.Fatal(err)
	}
	u := r.Usage
	if calls != 8 || u.Attempts != 8 || u.TokenReportedAttempts != 5 || u.CostReportedAttempts != 5 || *u.InputTokens != 500 || *u.OutputTokens != 200 || *u.TotalTokens != 700 || *u.ReasoningTokens != 150 || *u.CachedTokens != 100 || math.Abs(*u.CostUSD-.005) > 1e-12 {
		t.Fatalf("bad accounting: %+v calls=%d", u, calls)
	}
	if r.Results[0].Metrics.Excluded != 1 || r.Results[0].Metrics.Errors != 1 || r.Results[0].Requests.Retries != 2 {
		t.Fatalf("scoring changed: %+v", r.Results[0])
	}
	if !strings.Contains(u.String(), "incomplete; 5/8 attempts reported") || !strings.Contains(u.String(), "$0.00500000") {
		t.Fatal(u.String())
	}
	dir := t.TempDir()
	if err := WriteReport(dir, r); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "results.json"))
	if err != nil {
		t.Fatal(err)
	}
	var saved Report
	if err := json.Unmarshal(b, &saved); err != nil {
		t.Fatal(err)
	}
	if saved.Version != 4 || *saved.Usage.TotalTokens != 700 || *saved.Results[0].Usage.CostUSD != *u.CostUSD || saved.Results[0].Batches[0].Attempts[0].Usage == nil {
		t.Fatal("lost persisted usage")
	}
	f, err := os.Open(filepath.Join(dir, "summary.csv"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	rows, err := csv.NewReader(f).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	cells := map[string]string{}
	for i, k := range rows[0] {
		cells[k] = rows[1][i]
	}
	if cells["total_tokens"] != "700" || cells["cost_usd"] != "0.005" || cells["cost_reported_attempts"] != "5" {
		t.Fatalf("CSV=%v", cells)
	}
}

func TestUsageTotalsMultipleVariantsAndCancellation(t *testing.T) {
	for _, cancelRun := range []bool{false, true} {
		c, d := fixture()
		c.BatchSizes = []int{1, 2}
		c.Concurrency = 1
		ctx, cancel := context.WithCancel(context.Background())
		calls := 0
		r, err := Run(ctx, c, d, "", func(Model, Prompt) (ai.Provider, error) {
			return meteredFunc(func(_ context.Context, items []ai.Item, _ []ai.CategoryDef) ([]string, *ai.Usage, error) {
				calls++
				if cancelRun {
					cancel()
					return nil, testUsage(), context.Canceled
				}
				names := make([]string, len(items))
				for i := range names {
					names[i] = "Dining"
				}
				return names, testUsage(), nil
			}), nil
		}, nil)
		cancel()
		if (err != nil) != cancelRun {
			t.Fatal(err)
		}
		if r.Usage.Attempts != calls || *r.Usage.TotalTokens != int64(calls*140) || math.Abs(*r.Usage.CostUSD-float64(calls)*.001) > 1e-12 {
			t.Fatalf("incorrect totals: %+v", r.Usage)
		}
		if !cancelRun && (calls != 5 || len(r.Results) != 2) {
			t.Fatal("matrix not exercised")
		}
	}
}

func TestUsageMissingVersusExplicitZero(t *testing.T) {
	var u UsageTotals
	u.addBatches([]Batch{{Attempts: []Attempt{{}}}})
	if u.CostUSD != nil || u.TotalTokens != nil || !strings.Contains(u.String(), "reported cost: n/a") || usageCell(u.CostUSD) != "" {
		t.Fatalf("unknown treated as zero: %s", u)
	}
	var free UsageTotals
	free.addBatches([]Batch{{Attempts: []Attempt{{Usage: &ai.Usage{InputTokens: ptr(int64(0)), OutputTokens: ptr(int64(0)), TotalTokens: ptr(int64(0)), CostUSD: ptr(0.0)}}}}})
	if *free.CostUSD != 0 || free.TokenReportedAttempts != 1 || free.CostReportedAttempts != 1 || !strings.Contains(free.String(), "$0.00000000") || strings.Contains(free.String(), "incomplete") || usageCell(free.CostUSD) != "0" {
		t.Fatal(free.String())
	}
}
