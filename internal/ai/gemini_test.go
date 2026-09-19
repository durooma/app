package ai

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestGeminiRetainsRetryAfterAndRejectsShortResponse(t *testing.T) {
	for _, tc := range []struct {
		name        string
		status      int
		body, retry string
		wantRetry   time.Duration
	}{
		{"quota", 429, "", "3600", time.Hour},
		{"short response", 200, `{"candidates":[{"content":{"parts":[{"text":"[]"}]}}]}`, "", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := NewGemini("test-key", "test-model")
			g.client.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: tc.status, Header: http.Header{"Retry-After": []string{tc.retry}}, Body: io.NopCloser(strings.NewReader(tc.body))}, nil
			})
			_, err := g.Classify(context.Background(), []Item{{Desc: "test"}}, nil)
			if err == nil {
				t.Fatal("expected error")
			}
			if tc.wantRetry > 0 {
				var apiErr *APIError
				if !errors.As(err, &apiErr) || apiErr.RetryAfter != tc.wantRetry {
					t.Fatalf("retry lost: %v", err)
				}
			}
		})
	}
}
