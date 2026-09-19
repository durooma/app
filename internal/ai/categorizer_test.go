package ai

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"durooma/internal/models"
)

// fakeStore is an in-memory storeIface recording the categories written.
type fakeStore struct {
	cats    []models.Category
	rules   []models.Rule
	written map[int64]int64
}

func (f *fakeStore) ListCategories(context.Context) ([]models.Category, error) {
	return f.cats, nil
}

func (f *fakeStore) ListRules(context.Context) ([]models.Rule, error) { return f.rules, nil }

func (f *fakeStore) SetUncategorizedCategory(_ context.Context, id int64, categoryID *int64) error {
	if f.written == nil {
		f.written = map[int64]int64{}
	}
	f.written[id] = *categoryID
	return nil
}

func (f *fakeStore) PendingTransactions(_ context.Context, ids []int64) ([]models.Transaction, error) {
	var txns []models.Transaction
	for _, id := range ids {
		if _, exists := f.written[id]; !exists {
			txns = append(txns, models.Transaction{ID: id, Description: fmt.Sprintf("txn %d", id-1)})
		}
	}
	return txns, nil
}

// fakeProvider answers every item with the same category and counts calls. The
// optional hook runs before each answer, so a test can cancel mid-run.
type fakeProvider struct {
	answer string
	calls  int
	hook   func(ctx context.Context, call int)
}

func (p *fakeProvider) Name() string { return "fake" }

func (p *fakeProvider) Classify(ctx context.Context, items []Item, _ []CategoryDef) ([]string, error) {
	p.calls++
	if p.hook != nil {
		p.hook(ctx, p.calls)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	out := make([]string, len(items))
	for i := range out {
		out[i] = p.answer
	}
	return out, nil
}

func uncategorized(n int) []models.Transaction {
	txns := make([]models.Transaction, n)
	for i := range txns {
		txns[i] = models.Transaction{ID: int64(i + 1), Description: fmt.Sprintf("txn %d", i)}
	}
	return txns
}

func newFixture(answer string) (*fakeStore, *fakeProvider, *Service) {
	st := &fakeStore{cats: []models.Category{{ID: 3, Name: "Groceries"}}}
	pr := &fakeProvider{answer: answer}
	return st, pr, NewService(st, pr)
}

// The progress callback drives a progress bar, so it has to publish the
// denominator before the first slow provider call and end at Done == Total.
func TestCategorizeProgressCoversWholeRun(t *testing.T) {
	st, _, svc := newFixture("Groceries")
	st.rules = []models.Rule{{Pattern: "txn 0", CategoryID: 3}}

	var got []Progress
	rep, err := svc.CategorizeWithProgress(context.Background(), uncategorized(20),
		func(p Progress) { got = append(got, p) })
	if err != nil {
		t.Fatal(err)
	}

	if len(got) == 0 {
		t.Fatal("no progress reported")
	}
	if first := got[0]; first.Done != 0 || first.Total != 20 {
		t.Errorf("first progress = %+v, want the total known up front with nothing done", first)
	}
	if last := got[len(got)-1]; last.Done != 20 || last.Total != 20 {
		t.Errorf("last progress = %+v, want 20 of 20", last)
	}
	for i, p := range got {
		if i > 0 && p.Done < got[i-1].Done {
			t.Errorf("progress went backwards at %d: %+v after %+v", i, p, got[i-1])
		}
	}
	if rep.ByRules != 1 || rep.ByAI != 19 {
		t.Errorf("report = %+v, want 1 by rules and 19 by AI", rep)
	}
}

// Aborting has to stop the run promptly and keep what it already wrote, rather
// than burning through the remaining batches.
func TestCategorizeAbortStopsEarlyAndKeepsWrites(t *testing.T) {
	st, pr, svc := newFixture("Groceries")
	ctx, cancel := context.WithCancel(context.Background())
	pr.hook = func(_ context.Context, call int) {
		if call == 2 {
			cancel() // abort while the second batch is in flight
		}
	}

	// 5 batches of itemsPerPrompt; the abort must land well before the last.
	rep, err := svc.CategorizeWithProgress(ctx, uncategorized(5*itemsPerPrompt), nil)
	defer cancel()

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if pr.calls > 2 {
		t.Errorf("provider called %d times, want the run to stop at the aborted batch", pr.calls)
	}
	if rep.ByAI != itemsPerPrompt {
		t.Errorf("ByAI = %d, want the %d transactions of the completed batch", rep.ByAI, itemsPerPrompt)
	}
	if len(st.written) != itemsPerPrompt {
		t.Errorf("wrote %d categories, want the completed batch kept", len(st.written))
	}
	// A cancelled batch must not be reported as an AI answer we chose to skip.
	if rep.Unresolved != 0 {
		t.Errorf("Unresolved = %d, want 0: the run stopped rather than giving up on batches", rep.Unresolved)
	}
}

// A provider that fails for its own reasons still lets the run continue, unlike
// an abort.
func TestCategorizeProviderErrorLeavesBatchUnresolved(t *testing.T) {
	st := &fakeStore{cats: []models.Category{{ID: 3, Name: "Groceries"}}}
	svc := NewService(st, errProvider{})

	rep, err := svc.CategorizeWithProgress(context.Background(), uncategorized(itemsPerPrompt), nil)
	if err != nil {
		t.Fatalf("a provider error should not fail the whole run: %v", err)
	}
	if rep.Unresolved != itemsPerPrompt {
		t.Errorf("Unresolved = %d, want the whole failed batch", rep.Unresolved)
	}
}

type errProvider struct{}

func (errProvider) Name() string { return "err" }
func (errProvider) Classify(context.Context, []Item, []CategoryDef) ([]string, error) {
	return nil, errors.New("provider exploded")
}
