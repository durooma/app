package ai

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestBackgroundPreferenceControlsLifecycle(t *testing.T) {
	var enabled, fail atomic.Bool
	checked := make(chan struct{}, 10)
	started := make(chan context.Context, 10)
	controller := NewBackgroundControl(func(context.Context) (bool, error) {
		checked <- struct{}{}
		if fail.Load() {
			return false, errors.New("database unavailable")
		}
		return enabled.Load(), nil
	}, func(ctx context.Context) {
		started <- ctx
		<-ctx.Done()
	}, time.Hour)
	root, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); controller.Run(root) }()
	defer func() {
		cancel()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Error("controller did not stop")
		}
	}()
	waitCheck := func() {
		t.Helper()
		select {
		case <-checked:
		case <-time.After(time.Second):
			t.Fatal("preference was not checked")
		}
	}
	waitStart := func() context.Context {
		t.Helper()
		select {
		case ctx := <-started:
			return ctx
		case <-time.After(time.Second):
			t.Fatal("worker did not start")
		}
		return nil
	}
	waitStop := func(ctx context.Context) {
		t.Helper()
		select {
		case <-ctx.Done():
		case <-time.After(time.Second):
			t.Fatal("worker did not cancel")
		}
	}
	waitCheck()
	// Another check after startup proves the disabled loop is still responsive.
	controller.Notify()
	waitCheck()
	select {
	case <-started:
		t.Fatal("background work started without opt-in")
	default:
	}

	enabled.Store(true)
	controller.Notify()
	waitCheck()
	first := waitStart()
	enabled.Store(false)
	controller.Notify()
	waitCheck()
	waitStop(first)

	enabled.Store(true)
	controller.Notify()
	waitCheck()
	second := waitStart()
	fail.Store(true)
	controller.Notify()
	waitCheck()
	waitStop(second)

	fail.Store(false)
	controller.Notify()
	waitCheck()
	third := waitStart()
	cancel()
	waitStop(third)
}

func TestBackgroundUsesSavedPreferenceOnStartup(t *testing.T) {
	started := make(chan struct{})
	controller := NewBackgroundControl(func(context.Context) (bool, error) { return true, nil },
		func(ctx context.Context) { close(started); <-ctx.Done() }, time.Hour)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { defer close(done); controller.Run(ctx) }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("saved opt-in was ignored")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("shutdown did not finish")
	}
}
