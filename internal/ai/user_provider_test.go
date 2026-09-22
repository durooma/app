package ai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"durooma/internal/models"
)

type userSettingsFake struct {
	settings           models.AISettings
	key                string
	delays             []time.Duration
	reserved, finished int
}

func (f *userSettingsFake) AISettings(context.Context, string) (models.AISettings, error) {
	return f.settings, nil
}
func (f *userSettingsFake) OpenRouterKey(context.Context, string) (string, error) { return f.key, nil }
func (f *userSettingsFake) ReserveUserAIRequest(_ context.Context, _ string, _ int64, spacing time.Duration) (time.Duration, error) {
	f.reserved++
	if spacing != time.Duration(f.settings.IntervalMS)*time.Millisecond {
		panic("wrong pacing")
	}
	if len(f.delays) > 0 {
		d := f.delays[0]
		f.delays = f.delays[1:]
		return d, nil
	}
	return 0, nil
}
func (f *userSettingsFake) FinishUserAIRequest(context.Context, string, int64, bool, time.Duration) error {
	f.finished++
	return nil
}
func TestUserProviderOwnKeyZDRRetriesAndRefresh(t *testing.T) {
	f := &userSettingsFake{settings: models.DefaultAISettings()}
	p := NewUserProvider(f, "owner")
	calls := 0
	p.client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Header.Get("Authorization") != "Bearer "+f.key {
			t.Fatal("wrong credential")
		}
		var body struct {
			Model    string            `json:"model"`
			Provider OpenRouterRouting `json:"provider"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.Model != f.settings.Model || !body.Provider.ZDR || body.Provider.DataCollection != "deny" {
			t.Fatal("model/privacy settings missing")
		}
		if calls == 1 {
			return &http.Response{StatusCode: 429, Body: io.NopCloser(strings.NewReader(`{"error":{"message":"sensitive-key"}}`))}, nil
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"choices":[{"message":{"content":"[\"Dining\"]"}}]}`))}, nil
	})
	t.Setenv("AI_API_KEY", "must-never-be-used")
	if _, err := p.Classify(context.Background(), []Item{{Desc: "cafe"}}, nil); err != ErrAPIKeyRequired || calls != 0 {
		t.Fatal("environment key enabled categorization")
	}
	f.settings.HasKey = true
	f.key = "first-key"
	if _, err := p.Classify(context.Background(), []Item{{Desc: "cafe"}}, nil); err != nil || calls != 2 || f.finished != 2 {
		t.Fatalf("retry failed: %v calls=%d", err, calls)
	}
	f.key = "replacement-key"
	f.settings.Model = "z-ai/glm-5.3-flash"
	f.settings.Revision++
	if _, err := p.Classify(context.Background(), []Item{{Desc: "cafe"}}, nil); err != nil {
		t.Fatal(err)
	}
	f.settings.HasKey = false
	f.key = ""
	before := calls
	if _, err := p.Classify(context.Background(), []Item{{Desc: "cafe"}}, nil); err != ErrAPIKeyRequired || calls != before {
		t.Fatal("removed key still usable")
	}
}
func TestUserProviderCancellationAndMidflightSettingsChange(t *testing.T) {
	f := &userSettingsFake{settings: models.DefaultAISettings(), key: "own-key", delays: []time.Duration{time.Hour}}
	f.settings.HasKey = true
	p := NewUserProvider(f, "owner")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := p.Classify(ctx, []Item{{}}, nil); err != context.Canceled || f.reserved != 0 {
		t.Fatal("cancelled work reserved a request")
	}
	f.delays = nil
	p.client.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		f.settings.Revision++
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"choices":[{"message":{"content":"[\"Dining\"]"}}]}`))}, nil
	})
	if _, err := p.Classify(context.Background(), []Item{{}}, nil); err != ErrSettingsChanged {
		t.Fatal("stale model response accepted")
	}
}
