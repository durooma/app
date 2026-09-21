package eval

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"net"
	"sync"
	"time"

	"durooma/internal/ai"
)

type RetryPolicy struct {
	MaxAttempts    int    `json:"max_attempts"`
	InitialBackoff string `json:"initial_backoff"`
	MaxBackoff     string `json:"max_backoff"`
}

func (p RetryPolicy) defaults() RetryPolicy {
	if p.MaxAttempts == 0 {
		p.MaxAttempts = 5
	}
	if p.InitialBackoff == "" {
		p.InitialBackoff = "2s"
	}
	if p.MaxBackoff == "" {
		p.MaxBackoff = "30s"
	}
	return p
}
func (p RetryPolicy) validate() error {
	p = p.defaults()
	initial, err := time.ParseDuration(p.InitialBackoff)
	cap, err2 := time.ParseDuration(p.MaxBackoff)
	if p.MaxAttempts < 1 || p.MaxAttempts > 20 || err != nil || err2 != nil || initial <= 0 || cap < initial || cap > time.Hour {
		return fmt.Errorf("retry requires max_attempts 1–20 and positive initial_backoff <= max_backoff <= 1h")
	}
	return nil
}
func (p RetryPolicy) delay(attempt int, retryAfter time.Duration) time.Duration {
	p = p.defaults()
	delay, _ := time.ParseDuration(p.InitialBackoff)
	cap, _ := time.ParseDuration(p.MaxBackoff)
	for n := 1; n < attempt; n++ {
		delay = min(cap, delay*2)
	}
	delay = min(delay, cap)
	// Equal jitter spreads retries without exceeding the configured cap.
	jitter := delay / 2
	if jitter > 0 {
		delay = jitter + time.Duration(rand.Int64N(int64(delay-jitter)+1))
	}
	return max(delay, retryAfter)
}

type Attempt struct {
	Usage        *ai.Usage `json:"usage,omitempty"`
	Number       int       `json:"number"`
	LatencyMS    float64   `json:"latency_ms"`
	Error        string    `json:"error,omitempty"`
	Retryable    bool      `json:"retryable,omitempty"`
	Status       int       `json:"http_status,omitempty"`
	RetryAfterMS float64   `json:"retry_after_ms,omitempty"`
}

// RequestGate applies spacing to EVERY attempt, including retries. A rate-limit
// response extends a shared cooldown so new batches cannot bypass backoff.
type requestGate struct {
	mu             sync.Mutex
	interval       time.Duration
	next, cooldown time.Time
}

func (g *requestGate) enter(ctx context.Context, notBefore time.Time) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		g.mu.Lock()
		at := g.next
		if g.cooldown.After(at) {
			at = g.cooldown
		}
		if notBefore.After(at) {
			at = notBefore
		}
		delay := time.Until(at)
		if delay <= 0 {
			g.next = time.Now().Add(g.interval)
			g.mu.Unlock()
			return ctx.Err()
		}
		g.mu.Unlock()
		if err := wait(ctx, delay); err != nil {
			return err
		}
	}
}
func (g *requestGate) pause(delay time.Duration) {
	g.mu.Lock()
	defer g.mu.Unlock()
	until := time.Now().Add(delay)
	if until.After(g.cooldown) {
		g.cooldown = until
	}
}

// Infrastructure failures are excluded from model scores. Deterministic API
// rejections (e.g. 401) are also separate, but are not retried.
func requestFailure(err error) (infrastructure, retryable bool, after time.Duration, status int) {
	if err == nil {
		return
	}
	var apiErr *ai.APIError
	if errors.As(err, &apiErr) {
		status = apiErr.Status
		after = apiErr.RetryAfter
		return true, status == 408 || status == 429 || status >= 500 && status <= 599, after, status
	}
	if errors.Is(err, context.Canceled) {
		return true, false, 0, 0
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true, true, 0, 0
	}
	var transport *ai.NetworkError
	var network net.Error
	if errors.As(err, &transport) || errors.As(err, &network) {
		return true, true, 0, 0
	}
	return false, false, 0, 0
}

func classifyWithRetry(ctx context.Context, p ai.Provider, items []ai.Item, defs []ai.CategoryDef, timeout time.Duration, policy RetryPolicy, gate *requestGate) ([]string, Batch, error) {
	policy = policy.defaults()
	b := Batch{Size: len(items)}
	started := time.Now()
	var notBefore time.Time
	for {
		if err := gate.enter(ctx, notBefore); err != nil {
			b.Excluded = true
			b.Error = err.Error()
			b.ElapsedMS = float64(time.Since(started).Microseconds()) / 1000
			return nil, b, err
		}
		callCtx, cancel := context.WithTimeout(ctx, timeout)
		began := time.Now()
		var names []string
		var usage *ai.Usage
		var err error
		if metered, ok := p.(ai.UsageProvider); ok {
			names, usage, err = metered.ClassifyWithUsage(callCtx, items, defs)
		} else {
			names, err = p.Classify(callCtx, items, defs)
		}
		// The timeout belongs to this attempt, not its later retry or the whole run.
		if callCtx.Err() != nil {
			err = callCtx.Err()
		}
		cancel()
		if err == nil && len(names) != len(items) {
			err = fmt.Errorf("expected %d categories, got %d", len(items), len(names))
		}
		infra, retryable, after, status := requestFailure(err)
		attempt := Attempt{Usage: usage, Number: len(b.Attempts) + 1, LatencyMS: float64(time.Since(began).Microseconds()) / 1000, Retryable: retryable, Status: status, RetryAfterMS: float64(after.Milliseconds())}
		if err != nil {
			attempt.Error = err.Error()
		}
		b.Attempts = append(b.Attempts, attempt)
		b.LatencyMS += attempt.LatencyMS
		delay := policy.delay(attempt.Number, after)
		if status == 429 {
			gate.pause(delay)
		}
		if err == nil || !retryable || ctx.Err() != nil || len(b.Attempts) >= policy.MaxAttempts {
			b.Excluded = infra
			b.RetryExhausted = retryable && ctx.Err() == nil && len(b.Attempts) >= policy.MaxAttempts
			if err != nil {
				b.Error = err.Error()
			}
			b.ElapsedMS = float64(time.Since(started).Microseconds()) / 1000
			return names, b, err
		}
		notBefore = time.Now().Add(delay)
	}
}
