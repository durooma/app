package ai

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestResetCancelsInflightAndBlocksWorkUntilCleared(t *testing.T) {
	st, _, _ := newFixture("Groceries")
	p := &concurrentProvider{started: make(chan struct{}, 20), release: make(chan struct{})}
	svc := NewService(st, p)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := svc.Categorize(ctx, uncategorized(1)); done <- err }()
	select {
	case <-p.started:
	case <-ctx.Done():
		t.Fatal("run not started")
	}
	resetStarted := make(chan struct{})
	releaseReset := make(chan struct{})
	resetDone := make(chan error, 1)
	go func() {
		resetDone <- svc.Reset(ctx, func(context.Context) error {
			close(resetStarted)
			select {
			case <-releaseReset:
			case <-ctx.Done():
				return ctx.Err()
			}
			clear(st.written)
			return nil
		})
	}()
	select {
	case <-resetStarted:
	case <-ctx.Done():
		t.Fatal("reset failed to stop active run")
	}
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal("active run not canceled", err)
	}
	if len(st.written) != 0 {
		t.Fatal("stale labels written")
	}
	if _, err := svc.Categorize(ctx, uncategorized(1)); !errors.Is(err, context.Canceled) {
		t.Fatal("work accepted during reset")
	}
	if err := svc.Reset(ctx, func(context.Context) error { return nil }); err == nil {
		t.Fatal("overlapping reset accepted")
	}
	close(releaseReset)
	if err := <-resetDone; err != nil {
		t.Fatal(err)
	}
	close(p.release)
	if _, err := svc.Categorize(ctx, uncategorized(1)); err != nil || len(st.written) != 1 {
		t.Fatal("fresh run failed", err)
	}
}

func TestResetWaitsForRetryBookkeeping(t *testing.T) {
	st, p, _ := newFixture("Groceries")
	svc := NewService(st, p)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	finishing := make(chan struct{})
	release := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		svc.categorizeWithProgress(ctx, uncategorized(1), nil, func(context.Context) { close(finishing); <-release })
	}()
	<-finishing
	resetDone := make(chan error, 1)
	go func() { resetDone <- svc.Reset(ctx, func(context.Context) error { clear(st.written); return nil }) }()
	select {
	case <-resetDone:
		t.Fatal("reset overtook retry bookkeeping")
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	<-finished
	if err := <-resetDone; err != nil {
		t.Fatal(err)
	}
	if len(st.written) != 0 {
		t.Fatal("reset did not clear completed writes")
	}
}
