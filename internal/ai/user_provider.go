package ai

import (
	"context"
	"errors"
	"net"
	"net/http"
	"time"

	"durooma/internal/models"
)

var ErrAPIKeyRequired = errors.New("add your OpenRouter API key in Settings to enable categorization")
var ErrSettingsChanged = errors.New("categorization settings changed; start a new run")

type userProviderStore interface {
	AISettings(context.Context, string) (models.AISettings, error)
	OpenRouterKey(context.Context, string) (string, error)
	ReserveUserAIRequest(context.Context, string, int64, time.Duration) (time.Duration, error)
	FinishUserAIRequest(context.Context, string, int64, bool, time.Duration) error
}

// UserProvider reads the user's credentials for each request; environment keys
// are never consulted. One client is shared across concurrent workers.
type UserProvider struct {
	store  userProviderStore
	userID string
	client *http.Client
}

func NewUserProvider(st userProviderStore, userID string) *UserProvider {
	return &UserProvider{store: st, userID: userID, client: newEvalHTTPClient()}
}
func (p *UserProvider) Name() string { return "OpenRouter" }
func (p *UserProvider) Classify(ctx context.Context, items []Item, defs []CategoryDef) ([]string, error) {
	return p.request(ctx, func(ctx context.Context, o *OpenRouter) ([]string, error) { return o.Classify(ctx, items, defs) })
}

// ClassifyPeriods suggests periods under the same key, pacing and retries as
// categorization; both draw from one request budget.
func (p *UserProvider) ClassifyPeriods(ctx context.Context, items []PeriodItem) ([]string, error) {
	return p.request(ctx, func(ctx context.Context, o *OpenRouter) ([]string, error) { return o.ClassifyPeriods(ctx, items) })
}

// request runs one paced model call with the user's current credentials,
// retrying transient failures.
func (p *UserProvider) request(ctx context.Context, call func(context.Context, *OpenRouter) ([]string, error)) ([]string, error) {
	settings, err := p.store.AISettings(ctx, p.userID)
	if err != nil {
		return nil, errors.New("could not load categorization settings")
	}
	if !settings.HasKey {
		return nil, ErrAPIKeyRequired
	}
	for attempt := 1; attempt <= 5; attempt++ {
		for {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			delay, err := p.store.ReserveUserAIRequest(ctx, p.userID, settings.Revision, time.Duration(settings.IntervalMS)*time.Millisecond)
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
		key, err := p.store.OpenRouterKey(ctx, p.userID)
		if err != nil {
			return nil, err
		}
		if key == "" {
			return nil, ErrAPIKeyRequired
		}
		current, err := p.store.AISettings(ctx, p.userID)
		if err != nil {
			return nil, errors.New("could not load categorization settings")
		}
		if current.Revision != settings.Revision {
			return nil, ErrSettingsChanged
		}
		provider := NewOpenRouterWithRouting(key, settings.Model, "", &OpenRouterRouting{ZDR: true, DataCollection: "deny"})
		provider.client = p.client
		callCtx, cancel := context.WithTimeout(ctx, 120*time.Second)
		names, err := call(callCtx, provider)
		if callCtx.Err() != nil {
			err = callCtx.Err()
		}
		cancel()
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		var apiErr *APIError
		var network *NetworkError
		var netErr net.Error
		retry := errors.As(err, &network) || errors.As(err, &netErr) || errors.Is(err, context.DeadlineExceeded)
		var after time.Duration
		if errors.As(err, &apiErr) {
			after = apiErr.RetryAfter
			retry = apiErr.Status == 408 || apiErr.Status == 429 || apiErr.Status >= 500 && apiErr.Status <= 599
		}
		if saveErr := p.store.FinishUserAIRequest(ctx, p.userID, settings.Revision, retry, after); saveErr != nil {
			return nil, errors.New("could not persist request pacing")
		}
		current, readErr := p.store.AISettings(ctx, p.userID)
		if readErr != nil {
			return nil, errors.New("could not load categorization settings")
		}
		if !current.HasKey || current.Revision != settings.Revision {
			return nil, ErrSettingsChanged
		}
		if !retry || attempt == 5 {
			return names, err
		}
	}
	panic("unreachable")
}
