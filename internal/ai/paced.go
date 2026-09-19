package ai

import (
	"context"
	"errors"
	"log"
	"strconv"
	"time"
)

type requestBudget interface {
	ReserveAIRequest(context.Context, time.Duration) (time.Duration, error)
	FinishAIRequest(context.Context, bool, time.Duration) error
}

// PacedProvider gives every caller the same durable request budget. Spacing is
// also derived from the daily allowance, spreading it over the full day.
type PacedProvider struct {
	provider Provider
	budget   requestBudget
	spacing  time.Duration
}

func NewPacedProvider(p Provider, budget requestBudget, interval time.Duration, requestsPerDay int) *PacedProvider {
	return &PacedProvider{provider: p, budget: budget,
		spacing: max(interval, (24*time.Hour+time.Duration(requestsPerDay)-1)/time.Duration(requestsPerDay))}
}

func (p *PacedProvider) Name() string { return p.provider.Name() }

func (p *PacedProvider) Classify(ctx context.Context, items []Item, categories []CategoryDef) ([]string, error) {
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		delay, err := p.budget.ReserveAIRequest(ctx, p.spacing)
		if err != nil {
			return nil, err
		}
		if delay == 0 {
			break
		}
		if !wait(ctx, delay) {
			return nil, ctx.Err()
		}
	}
	names, err := p.provider.Classify(ctx, items, categories)
	// A reserved attempt remains spent even when its caller cancels.
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	var retry time.Duration
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		retry = apiErr.RetryAfter
	}
	if saveErr := p.budget.FinishAIRequest(ctx, err != nil, retry); saveErr != nil {
		return nil, saveErr
	}
	if err != nil {
		log.Printf("AI request failed; retry cooldown saved: %v", err)
	}
	return names, err
}

// APIError retains retry guidance without logging provider payloads or keys.
type APIError struct {
	Status     int
	RetryAfter time.Duration
}

func (e *APIError) Error() string {
	return "AI provider rejected the request (HTTP " + strconv.Itoa(e.Status) + ")"
}
