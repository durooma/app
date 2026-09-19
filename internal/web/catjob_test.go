package web

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"durooma/internal/ai"
	"durooma/internal/models"
)

// jobStore is the minimal store the categorization service needs.
type jobStore struct{}

func (jobStore) ListCategories(context.Context) ([]models.Category, error) {
	return []models.Category{{ID: 3, Name: "Groceries"}}, nil
}
func (jobStore) ListRules(context.Context) ([]models.Rule, error) { return nil, nil }
func (jobStore) SetUncategorizedCategory(context.Context, int64, *int64) error {
	return nil
}

func (jobStore) PendingTransactions(_ context.Context, ids []int64) ([]models.Transaction, error) {
	txns := make([]models.Transaction, len(ids))
	for i, id := range ids {
		txns[i] = models.Transaction{ID: id, Description: "Migros"}
	}
	return txns, nil
}

// blockingProvider parks inside Classify until released or cancelled, so a test
// can observe a run while it is genuinely in flight.
type blockingProvider struct {
	started chan struct{}
	release chan struct{}
}

func newBlockingProvider() *blockingProvider {
	return &blockingProvider{started: make(chan struct{}, 1), release: make(chan struct{})}
}

func (blockingProvider) Name() string { return "fake" }

func (p *blockingProvider) Classify(ctx context.Context, items []ai.Item, _ []ai.CategoryDef) ([]string, error) {
	select {
	case p.started <- struct{}{}:
	default:
	}
	select {
	case <-p.release:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	out := make([]string, len(items))
	for i := range out {
		out[i] = "Groceries"
	}
	return out, nil
}

// waitFor polls cond until it holds, failing the test on timeout. The job's
// terminal state is written by its own goroutine, so tests wait for it rather
// than sleeping a fixed amount.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestCatJobIdleReportsNothing(t *testing.T) {
	var j catJob
	if got := j.status(); got != nil {
		t.Errorf("idle status = %+v, want nil so no bar is rendered", got)
	}
}

// One run at a time: a second start must be refused rather than racing the
// first over the same transactions.
func TestCatJobRefusesConcurrentRun(t *testing.T) {
	p := newBlockingProvider()
	svc := ai.NewService(jobStore{}, p)
	var j catJob

	if !j.start(catRun{svc: svc, txns: []models.Transaction{{ID: 1, Description: "Migros"}}}) {
		t.Fatal("first start refused")
	}
	<-p.started
	if j.start(catRun{svc: svc, txns: []models.Transaction{{ID: 2, Description: "Coop"}}}) {
		t.Error("second start accepted while a run is in flight")
	}

	st := j.status()
	if st == nil || !st.Active {
		t.Fatalf("status = %+v, want an active run", st)
	}
	if st.Total != 1 || st.Provider != "fake" {
		t.Errorf("status = %+v, want 1 transaction attributed to the fake provider", st)
	}

	close(p.release)
	waitFor(t, "the run to finish", func() bool { return !j.status().Active })
}

// Aborting flips the bar to "aborting" immediately, then settles on a summary
// that reports the partial run — and dismissing clears it.
func TestCatJobAbortThenDismiss(t *testing.T) {
	p := newBlockingProvider()
	svc := ai.NewService(jobStore{}, p)
	var j catJob

	j.start(catRun{svc: svc, txns: []models.Transaction{{ID: 1, Description: "Migros"}}})
	<-p.started

	j.abort()
	waitFor(t, "the run to wind down", func() bool { return !j.status().Active })

	st := j.status()
	if !st.Aborted {
		t.Errorf("status = %+v, want the run marked aborted", st)
	}
	if st.Err != "" {
		t.Errorf("Err = %q, want an abort to read as aborted rather than failed", st.Err)
	}

	// The summary survives until dismissed, so it is still there after a page
	// navigation re-renders the layout.
	if j.status() == nil {
		t.Fatal("summary vanished before being dismissed")
	}
	j.dismiss()
	if got := j.status(); got != nil {
		t.Errorf("status after dismiss = %+v, want nil", got)
	}
}

// A completed run reports its numbers and stops asking to be polled.
func TestCatJobCompletes(t *testing.T) {
	p := newBlockingProvider()
	close(p.release) // never block
	svc := ai.NewService(jobStore{}, p)
	var j catJob

	j.start(catRun{
		svc:       svc,
		txns:      []models.Transaction{{ID: 1, Description: "Migros"}, {ID: 2, Description: "Coop"}},
		remaining: func(context.Context) (int, error) { return 7, nil },
	})
	waitFor(t, "the run to finish", func() bool { return !j.status().Active })

	st := j.status()
	if st.Aborted || st.Err != "" {
		t.Errorf("status = %+v, want a clean finish", st)
	}
	if st.Report.ByAI != 2 || st.Percent != 100 {
		t.Errorf("status = %+v, want both transactions categorized and a full bar", st)
	}
	// What the run did not reach has to be carried into the summary, otherwise
	// hitting the query bound looks like the whole job is done.
	if st.Remaining != 7 {
		t.Errorf("Remaining = %d, want the 7 still uncategorized", st.Remaining)
	}
}

// The remainder is counted after an abort too — that is precisely when the user
// needs to know how much is left.
func TestCatJobAbortStillCountsRemainder(t *testing.T) {
	p := newBlockingProvider()
	svc := ai.NewService(jobStore{}, p)
	var j catJob

	j.start(catRun{
		svc:  svc,
		txns: []models.Transaction{{ID: 1, Description: "Migros"}},
		// A cancelled run must not pass its cancelled context to the count.
		remaining: func(ctx context.Context) (int, error) {
			if err := ctx.Err(); err != nil {
				return 0, err
			}
			return 42, nil
		},
	})
	<-p.started
	j.abort()
	waitFor(t, "the run to wind down", func() bool { return !j.status().Active })

	if got := j.status().Remaining; got != 42 {
		t.Errorf("Remaining = %d, want 42 counted on a live context", got)
	}
}

// A failing count must not sink the run: the summary still reports the work.
func TestCatJobSurvivesFailedRemainderCount(t *testing.T) {
	p := newBlockingProvider()
	close(p.release)
	svc := ai.NewService(jobStore{}, p)
	var j catJob

	j.start(catRun{
		svc:       svc,
		txns:      []models.Transaction{{ID: 1, Description: "Migros"}},
		remaining: func(context.Context) (int, error) { return 0, errors.New("db down") },
	})
	waitFor(t, "the run to finish", func() bool { return !j.status().Active })

	st := j.status()
	if st.Err != "" || st.Report.ByAI != 1 {
		t.Errorf("status = %+v, want the run itself reported as successful", st)
	}
	if st.Remaining != 0 {
		t.Errorf("Remaining = %d, want 0 when the count could not be taken", st.Remaining)
	}
}

// The bar is what the user actually sees, so pin the parts each state has to
// carry: a poll trigger and an abort while running, neither once finished.
func TestRenderCatProgressStates(t *testing.T) {
	tmpls, err := loadTemplates()
	if err != nil {
		t.Fatal(err)
	}
	render := func(st *catStatus) string {
		t.Helper()
		var buf strings.Builder
		if err := tmpls.pages["transactions"].ExecuteTemplate(&buf, "cat-progress", st); err != nil {
			t.Fatalf("render cat-progress: %v", err)
		}
		return buf.String()
	}

	// Nothing to report: a hidden placeholder, so later swaps still have a target.
	idle := render(nil)
	if !strings.Contains(idle, `id="cat-progress"`) || !strings.Contains(idle, "hidden") {
		t.Errorf("idle bar should be a hidden placeholder, got:\n%s", idle)
	}
	if strings.Contains(idle, "hx-trigger") {
		t.Errorf("idle bar should not poll, got:\n%s", idle)
	}

	active := render(&catStatus{Active: true, Done: 30, Total: 120, Percent: 25, Provider: "gemini"})
	for _, want := range []string{
		`hx-get="/transactions/categorize/status"`, // keeps itself up to date
		`hx-trigger="every 1s"`,
		"/transactions/categorize/abort", // the abort the user asked for
		"30 of 120",
		"width: 25%",
		`class="spinner"`,
	} {
		if !strings.Contains(active, want) {
			t.Errorf("active bar missing %q:\n%s", want, active)
		}
	}

	aborting := render(&catStatus{Active: true, Aborting: true, Total: 120, Percent: 25})
	if !strings.Contains(aborting, "disabled") || !strings.Contains(aborting, "Aborting") {
		t.Errorf("aborting bar should say so and disable the button:\n%s", aborting)
	}

	done := render(&catStatus{Done: 120, Total: 120, Percent: 100,
		Report: ai.Report{Total: 120, ByRules: 20, ByAI: 90, Unresolved: 10, Provider: "gemini"}})
	for _, want := range []string{"Categorized 120", "20 by rules", "90 by AI", "10 left unresolved",
		"/transactions/categorize/dismiss"} {
		if !strings.Contains(done, want) {
			t.Errorf("finished bar missing %q:\n%s", want, done)
		}
	}
	if strings.Contains(done, "hx-trigger") {
		t.Errorf("finished bar must stop polling:\n%s", done)
	}
	if strings.Contains(done, "/categorize/abort") {
		t.Errorf("finished bar must not offer an abort:\n%s", done)
	}

	aborted := render(&catStatus{Done: 45, Total: 120, Percent: 37, Aborted: true,
		Report: ai.Report{Total: 120, ByRules: 15, ByAI: 30}})
	if !strings.Contains(aborted, "Aborted — 45 of 120") {
		t.Errorf("aborted bar should report the partial run:\n%s", aborted)
	}

	failed := render(&catStatus{Total: 120, Err: "provider exploded"})
	if !strings.Contains(failed, "provider exploded") {
		t.Errorf("failed bar should surface the error:\n%s", failed)
	}

	// Hitting the query bound must be visible, not silent.
	bounded := render(&catStatus{Done: 5000, Total: 5000, Percent: 100, Remaining: 300,
		Report: ai.Report{Total: 5000, ByAI: 5000, Provider: "gemini"}})
	if !strings.Contains(bounded, "<strong>300</strong> still uncategorized") {
		t.Errorf("a bounded run must report what it left behind:\n%s", bounded)
	}
	if strings.Contains(done, "still uncategorized") {
		t.Errorf("a run that finished the set should not claim a remainder:\n%s", done)
	}

	empty := render(&catStatus{Percent: 100, Report: ai.Report{Total: 0, Provider: "gemini"}})
	if !strings.Contains(empty, "Nothing to categorize") {
		t.Errorf("a run with no work should say so:\n%s", empty)
	}
}
