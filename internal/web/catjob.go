package web

import (
	"context"
	"errors"
	"sync"
	"time"

	"durooma/internal/ai"
	"durooma/internal/models"
)

// catJob tracks the single in-flight "categorize all" run. Categorizing a few
// hundred transactions is dozens of sequential provider round trips — far too
// slow to hold an HTTP request open — so the run happens in the background and
// the browser polls this state for a progress bar.
//
// One slot is enough: the app is single-user and self-hosted, so a second run
// started while one is active is refused rather than queued.
type catJob struct {
	mu       sync.Mutex
	active   bool
	aborting bool
	cancel   context.CancelFunc
	done     int
	total    int
	provider string

	// result is set when a run ends and kept until dismissed, so the outcome
	// survives navigating to another page.
	result *catResult
}

type catResult struct {
	report    ai.Report
	done      int
	remaining int
	aborted   bool
	err       string
}

// catRun is everything a run needs: the work, the service to do it with, and a
// way to ask what is still uncategorized once it ends.
type catRun struct {
	svc  *ai.Service
	txns []models.Transaction

	// remaining reports how many transactions matching the run's filter are
	// still uncategorized after it finishes — those left behind by the query
	// bound, plus any the run failed to resolve. May be nil.
	remaining func(ctx context.Context) (int, error)
}

// catStatus is the template's view of the job. A nil *catStatus means there is
// nothing to show, which renders as no bar at all.
type catStatus struct {
	Active   bool
	Aborting bool
	Done     int
	Total    int
	Percent  int
	Provider string

	// Set once the run has ended.
	Aborted   bool
	Err       string
	Remaining int
	Report    ai.Report
}

// start kicks off a run, reporting false if one is already in flight.
func (j *catJob) start(run catRun) bool {
	j.mu.Lock()
	if j.active {
		j.mu.Unlock()
		return false
	}
	// Deliberately not derived from the request context: that is cancelled the
	// moment the starting response is written, which would kill the run.
	ctx, cancel := context.WithCancel(context.Background())
	j.active, j.aborting = true, false
	j.cancel = cancel
	j.done, j.total = 0, len(run.txns)
	j.provider = run.svc.ProviderName()
	j.result = nil
	j.mu.Unlock()

	go func() {
		defer cancel()
		rep, err := run.svc.CategorizeWithProgress(ctx, run.txns, func(p ai.Progress) {
			j.mu.Lock()
			j.done, j.total = p.Done, p.Total
			j.mu.Unlock()
		})

		// Counted on a fresh context: an aborted run has cancelled ctx, and the
		// count is exactly what the user needs in that case.
		remaining := 0
		if run.remaining != nil {
			countCtx, cancelCount := context.WithTimeout(context.Background(), 10*time.Second)
			if n, cErr := run.remaining(countCtx); cErr == nil {
				remaining = n
			}
			cancelCount()
		}

		j.mu.Lock()
		defer j.mu.Unlock()
		res := &catResult{report: rep, done: j.done, remaining: remaining}
		switch {
		case errors.Is(err, context.Canceled):
			res.aborted = true
		case err != nil:
			res.err = err.Error()
		}
		j.active, j.aborting = false, false
		j.cancel = nil
		j.result = res
	}()
	return true
}

// abort asks the running job to stop. It returns once the cancellation is
// signalled, not once the run has wound down — the in-flight provider call
// still has to unwind, so the bar shows "Aborting…" until it does.
func (j *catJob) abort() {
	j.mu.Lock()
	cancel := j.cancel
	if j.active {
		j.aborting = true
	}
	j.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// dismiss clears a finished run's result so its bar stops being rendered. A
// running job is left alone.
func (j *catJob) dismiss() {
	j.mu.Lock()
	defer j.mu.Unlock()
	if !j.active {
		j.result = nil
	}
}

// status snapshots the job for rendering, returning nil when there is nothing
// to show.
func (j *catJob) status() *catStatus {
	j.mu.Lock()
	defer j.mu.Unlock()

	if j.active {
		return &catStatus{
			Active:   true,
			Aborting: j.aborting,
			Done:     j.done,
			Total:    j.total,
			Percent:  percent(j.done, j.total),
			Provider: j.provider,
		}
	}
	if j.result == nil {
		return nil
	}
	return &catStatus{
		Done:      j.result.done,
		Total:     j.total,
		Percent:   percent(j.result.done, j.total),
		Provider:  j.provider,
		Aborted:   j.result.aborted,
		Err:       j.result.err,
		Remaining: j.result.remaining,
		Report:    j.result.report,
	}
}

// percent renders done/total as a bar width, treating an empty run as complete
// so a zero-transaction run does not show an empty bar.
func percent(done, total int) int {
	if total <= 0 {
		return 100
	}
	if done >= total {
		return 100
	}
	return done * 100 / total
}
