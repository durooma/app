package ai

import (
	"context"
	"log"
	"time"
)

// BackgroundControl starts and stops automatic work according to a persistent
// user preference. Manual runs have independent contexts and are not cancelled.
// Only Run owns the worker lifecycle; notifications can safely be coalesced.
type BackgroundControl struct {
	enabled func(context.Context) (bool, error)
	work    func(context.Context)
	poll    time.Duration
	changed chan struct{}
}

func NewBackgroundControl(enabled func(context.Context) (bool, error), work func(context.Context), poll time.Duration) *BackgroundControl {
	return &BackgroundControl{enabled: enabled, work: work, poll: poll, changed: make(chan struct{}, 1)}
}

func (b *BackgroundControl) Notify() {
	select {
	case b.changed <- struct{}{}:
	default:
	}
}

// Run checks the setting at startup, on notification, and periodically. A failed
// preference read stops work rather than assuming the user opted in.
func (b *BackgroundControl) Run(ctx context.Context) {
	ticker := time.NewTicker(b.poll)
	defer ticker.Stop()
	var cancel context.CancelFunc
	var done chan struct{}
	stop := func() {
		if cancel != nil {
			cancel()
			<-done
			cancel, done = nil, nil
		}
	}
	defer stop()
	for ctx.Err() == nil {
		enabled, err := b.enabled(ctx)
		if err != nil {
			if ctx.Err() == nil {
				log.Printf("background categorization: read preference: %v", err)
			}
			enabled = false
		}
		if !enabled {
			stop()
		} else if cancel == nil && ctx.Err() == nil {
			workCtx, stopWork := context.WithCancel(ctx)
			cancel = stopWork
			done = make(chan struct{})
			go func(finished chan struct{}) {
				defer close(finished)
				defer stopWork()
				b.work(workCtx)
			}(done)
		}
		select {
		case <-ctx.Done():
			return
		case <-b.changed:
		case <-ticker.C:
		}
	}
}
