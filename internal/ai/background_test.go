package ai

import (
	"context"
	"errors"
	"testing"
	"time"

	"durooma/internal/models"
)

type queueFixture struct {
	*fakeStore
	reads    int
	deferred []int64
	cancel   context.CancelFunc
}

func (q *queueFixture) DueCategorization(context.Context, int) ([]models.Transaction, error) {
	q.reads++
	// Simulate an initially empty database, then an import with no UI action.
	if q.reads == 1 {
		return nil, nil
	}
	return uncategorized(3), nil
}
func (q *queueFixture) DeferCategorization(_ context.Context, ids []int64) error {
	q.deferred = ids
	q.cancel()
	return nil
}

func TestBackgroundDiscoversNewWorkAndStops(t *testing.T) {
	st, pr, svc := newFixture("Groceries")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	q := &queueFixture{fakeStore: st, cancel: cancel}
	done := make(chan struct{})
	go func() { defer close(done); RunBackground(ctx, q, svc, time.Millisecond) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("worker did not stop")
	}
	if len(st.written) != 3 || pr.calls != 1 || len(q.deferred) != 3 {
		t.Fatalf("written=%v calls=%d deferred=%v", st.written, pr.calls, q.deferred)
	}
}

func TestBackgroundFailureSchedulesRetry(t *testing.T) {
	st, _, _ := newFixture("Groceries")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	q := &queueFixture{fakeStore: st, cancel: cancel}
	RunBackground(ctx, q, NewService(st, errProvider{}), time.Millisecond)
	if len(q.deferred) != 3 || len(st.written) != 0 {
		t.Fatalf("failed request lost retry or wrote categories: %+v", q)
	}
}

type budgetFixture struct {
	delay    time.Duration
	spacing  time.Duration
	failed   bool
	retry    time.Duration
	reserved chan struct{}
}

func (b *budgetFixture) ReserveAIRequest(_ context.Context, spacing time.Duration) (time.Duration, error) {
	b.spacing = spacing
	if b.reserved != nil {
		close(b.reserved)
		b.reserved = nil
	}
	return b.delay, nil
}
func (b *budgetFixture) FinishAIRequest(_ context.Context, failed bool, retry time.Duration) error {
	b.failed, b.retry = failed, retry
	return nil
}

type limitedProvider struct{}

func (limitedProvider) Name() string { return "limited" }
func (limitedProvider) Classify(context.Context, []Item, []CategoryDef) ([]string, error) {
	return nil, &APIError{Status: 429, RetryAfter: 2 * time.Hour}
}

func TestPacedProviderHonorsDailyAllowanceAndPersistsRetry(t *testing.T) {
	b := &budgetFixture{}
	p := NewPacedProvider(limitedProvider{}, b, time.Minute, 200)
	_, err := p.Classify(context.Background(), []Item{{Desc: "test"}}, nil)
	var apiErr *APIError
	if !errors.As(err, &apiErr) || !b.failed || b.retry != 2*time.Hour {
		t.Fatalf("err=%v budget=%+v", err, b)
	}
	if b.spacing != 7*time.Minute+12*time.Second {
		t.Fatalf("spacing=%v", b.spacing)
	}
}

func TestPacedProviderCancellationDoesNotCallModel(t *testing.T) {
	reserved := make(chan struct{})
	b := &budgetFixture{delay: time.Hour, reserved: reserved}
	pr := &fakeProvider{}
	p := NewPacedProvider(pr, b, time.Minute, 200)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := p.Classify(ctx, nil, nil); done <- err }()
	<-reserved
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("quota wait ignored cancellation")
	}
	if pr.calls != 0 {
		t.Fatal("called model while budget unavailable")
	}
}

func TestServiceRefreshesStaleRunAndUsesConfiguredBatchSize(t *testing.T) {
	st, pr, _ := newFixture("Groceries")
	svc := NewServiceWithBatchSize(st, pr, 30)
	txns := uncategorized(65)
	rep, err := svc.Categorize(context.Background(), txns)
	if err != nil || rep.ByAI != 65 || pr.calls != 3 {
		t.Fatalf("%+v calls=%d err=%v", rep, pr.calls, err)
	}
	// A queued manual/automatic run may still have the old uncategorized snapshot.
	rep, err = svc.Categorize(context.Background(), txns)
	if err != nil || rep.Total != 0 || pr.calls != 3 {
		t.Fatalf("stale run repeated work: %+v calls=%d err=%v", rep, pr.calls, err)
	}
}

func TestConcurrentRunsShareWorkWithoutDuplicateRequests(t *testing.T) {
	st, pr, svc := newFixture("Groceries")
	started, release := make(chan struct{}), make(chan struct{})
	pr.hook = func(context.Context, int) { close(started); <-release }
	txns := uncategorized(3)
	done := make(chan error, 2)
	go func() { _, err := svc.Categorize(context.Background(), txns); done <- err }()
	<-started
	go func() { _, err := svc.Categorize(context.Background(), txns); done <- err }()
	close(release)
	for i := 0; i < 2; i++ {
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(time.Second):
			t.Fatal("concurrent run stuck")
		}
	}
	if pr.calls != 1 || len(st.written) != 3 {
		t.Fatalf("duplicated work: calls=%d written=%v", pr.calls, st.written)
	}
}
