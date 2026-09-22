package ai

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

type concurrentProvider struct {
	active, max atomic.Int32
	started     chan struct{}
	release     chan struct{}
}

func (p *concurrentProvider) Name() string { return "concurrent-test" }
func (p *concurrentProvider) Classify(ctx context.Context, items []Item, _ []CategoryDef) ([]string, error) {
	n := p.active.Add(1)
	defer p.active.Add(-1)
	for old := p.max.Load(); n > old; old = p.max.Load() {
		if p.max.CompareAndSwap(old, n) {
			break
		}
	}
	p.started <- struct{}{}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-p.release:
	}
	out := make([]string, len(items))
	for i := range out {
		out[i] = "Groceries"
	}
	return out, nil
}
func TestConfiguredServiceBoundsParallelCalls(t *testing.T) {
	st, _, _ := newFixture("Groceries")
	p := &concurrentProvider{started: make(chan struct{}, 20), release: make(chan struct{})}
	svc := NewConfiguredService(st, p, func(context.Context) (RunConfig, error) { return RunConfig{BatchSize: 1, Concurrency: 3}, nil })
	done := make(chan error, 1)
	go func() { _, err := svc.Categorize(context.Background(), uncategorized(20)); done <- err }()
	for i := 0; i < 3; i++ {
		select {
		case <-p.started:
		case <-time.After(time.Second):
			t.Fatal("provider requests did not run concurrently")
		}
	}
	select {
	case <-p.started:
		t.Fatal("exceeded concurrency limit")
	case <-time.After(20 * time.Millisecond):
	}
	close(p.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if p.max.Load() != 3 || len(st.written) != 20 {
		t.Fatal("incorrect parallel processing")
	}
}
func TestConfiguredServiceRejectsBeforeRulesAndCancelsActiveWork(t *testing.T) {
	st, _, _ := newFixture("Groceries")
	p := &concurrentProvider{started: make(chan struct{}, 20), release: make(chan struct{})}
	svc := NewConfiguredService(st, p, func(context.Context) (RunConfig, error) { return RunConfig{}, ErrAPIKeyRequired })
	if _, err := svc.Categorize(context.Background(), uncategorized(2)); err != ErrAPIKeyRequired || len(st.written) != 0 {
		t.Fatal("work started without owner key")
	}
	svc = NewConfiguredService(st, p, func(context.Context) (RunConfig, error) { return RunConfig{BatchSize: 1, Concurrency: 3}, nil })
	done := make(chan error, 1)
	go func() { _, err := svc.Categorize(context.Background(), uncategorized(20)); done <- err }()
	select {
	case <-p.started:
	case <-time.After(time.Second):
		t.Fatal("no active work")
	}
	svc.CancelActive()
	select {
	case err := <-done:
		if err != context.Canceled {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("active requests not canceled")
	}
	if len(st.written) != 0 {
		t.Fatal("canceled outputs written")
	}
}

// Invalid labels without a General fallback must not discard valid labels later
// in the same batch or prevent the progress counter from advancing.
type mixedLabelProvider struct{}

func (mixedLabelProvider) Name() string { return "mixed" }
func (mixedLabelProvider) Classify(context.Context, []Item, []CategoryDef) ([]string, error) {
	return []string{"invented", "Groceries"}, nil
}
func TestMixedLabelsProcessEntireBatch(t *testing.T) {
	st, _, _ := newFixture("Groceries")
	svc := NewService(st, mixedLabelProvider{})
	var final Progress
	rep, err := svc.CategorizeWithProgress(context.Background(), uncategorized(2), func(p Progress) { final = p })
	if err != nil || rep.Unresolved != 1 || rep.ByAI != 1 || len(st.written) != 1 || final.Done != 2 {
		t.Fatalf("rep=%+v progress=%+v err=%v", rep, final, err)
	}
}
