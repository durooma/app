package ai

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestOpenRouterRequestAndResponse(t *testing.T) {
	o := NewOpenRouter("secret", "vendor/model", "{{.Categories}}\n{{.Transactions}}")
	o.client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != "https://openrouter.ai/api/v1/chat/completions" || r.Method != "POST" || r.Header.Get("Authorization") != "Bearer secret" {
			t.Fatalf("wrong request endpoint/auth")
		}
		var b struct {
			Model    string                           `json:"model"`
			Stream   bool                             `json:"stream"`
			Messages []struct{ Role, Content string } `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
			t.Fatal(err)
		}
		if b.Model != "vendor/model" || b.Stream || len(b.Messages) != 1 || b.Messages[0].Role != "user" || !strings.Contains(b.Messages[0].Content, "cafe") || !strings.Contains(b.Messages[0].Content, "-7.00") {
			t.Fatalf("incorrect body: %+v", b)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("{\"choices\":[{\"finish_reason\":\"stop\",\"message\":{\"content\":\"```json\\n[\\\"Dining\\\"]\\n```\"}}]}"))}, nil
	})
	got, err := o.Classify(context.Background(), []Item{{Desc: "cafe", Amount: -7}}, []CategoryDef{{Name: "Dining", Description: "Restaurants"}})
	if err != nil || len(got) != 1 || got[0] != "Dining" {
		t.Fatalf("got=%v err=%v", got, err)
	}
}
func TestOpenRouterRejectsFailuresWithoutLeakingBody(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
	}{
		{"quota", `secret-provider-details`, 429},
		{"HTTP200 error", `{"error":{"message":"secret-provider-details"}}`, 200},
		{"truncated", `{"choices":[{"finish_reason":"length","message":{"content":"[\"Dining\"]"}}]}`, 200},
		{"missing choices", `{"choices":[]}`, 200},
		{"wrong count", `{"choices":[{"message":{"content":"[]"}}]}`, 200},
		{"non JSON content", `{"choices":[{"message":{"content":"secret-provider-details"}}]}`, 200},
		{"bad envelope", `secret-provider-details`, 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o := NewOpenRouter("secret-key", "vendor/model", "")
			o.client.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: tc.status, Body: io.NopCloser(strings.NewReader(tc.body))}, nil
			})
			_, err := o.Classify(context.Background(), []Item{{Desc: "test"}}, nil)
			if err == nil || strings.Contains(err.Error(), "secret") {
				t.Fatalf("err=%v", err)
			}
		})
	}
}

func TestOpenRouterPreservesRetryClassification(t *testing.T) {
	for _, tc := range []struct {
		name        string
		status      int
		body, retry string
		wantStatus  int
		network     bool
	}{
		{"rate limit", 429, "", "3", 429, false},
		{"upstream 429 inside 200", 200, `{"error":{"code":429,"message":"sensitive"}}`, "3", 429, false},
		{"service unavailable", 503, "", "3", 503, false},
		{"broken envelope", 200, `{"choices":`, "", 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o := NewOpenRouter("secret", "model", "")
			o.client.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: tc.status, Header: http.Header{"Retry-After": []string{tc.retry}}, Body: io.NopCloser(strings.NewReader(tc.body))}, nil
			})
			_, err := o.Classify(context.Background(), []Item{{Desc: "test"}}, nil)
			if tc.network {
				var e *NetworkError
				if !errors.As(err, &e) {
					t.Fatalf("untyped network error: %v", err)
				}
			} else {
				var e *APIError
				if !errors.As(err, &e) || e.Status != tc.wantStatus || e.RetryAfter != 3*time.Second {
					t.Fatalf("lost retry metadata: %v", err)
				}
			}
		})
	}
	future := time.Now().Add(time.Minute).UTC().Format(http.TimeFormat)
	if got := retryAfter(future); got < 58*time.Second || got > time.Minute {
		t.Fatalf("date Retry-After: %v", got)
	}
	if retryAfter("-1") != 0 || retryAfter("garbage") != 0 {
		t.Fatal("bad Retry-After accepted")
	}
}

func TestOpenRouterEnforcesZDRAndProviderOnEveryCall(t *testing.T) {
	routing := &OpenRouterRouting{ZDR: true, DataCollection: "deny", Only: []string{"fireworks"}}
	o := NewOpenRouterWithRouting("secret", "deepseek/deepseek-v4.1-flash", "", routing)
	// Caller mutation must not weaken the client's immutable routing constraints.
	routing.ZDR = false
	routing.Only[0] = "another-provider"
	calls := 0
	o.client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		var body struct {
			Provider OpenRouterRouting `json:"provider"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if !body.Provider.ZDR || body.Provider.DataCollection != "deny" || len(body.Provider.Only) != 1 || body.Provider.Only[0] != "fireworks" {
			t.Fatalf("privacy constraints absent: %+v", body)
		}
		calls++
		if calls == 1 {
			return &http.Response{StatusCode: 503, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(""))}, nil
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"choices":[{"message":{"content":"[\"Dining\"]"}}]}`))}, nil
	})
	if _, err := o.Classify(context.Background(), []Item{{Desc: "test"}}, nil); err == nil {
		t.Fatal("expected unavailable endpoint error")
	}
	got, err := o.Classify(context.Background(), []Item{{Desc: "test"}}, nil)
	if err != nil || len(got) != 1 || calls != 2 {
		t.Fatalf("got=%v err=%v calls=%d", got, err, calls)
	}
}

func TestOpenRouterRecordsUsageEvenWhenClassificationFails(t *testing.T) {
	for _, tc := range []struct {
		name, payload string
		status        int
		wantError     bool
	}{
		{"success", `"choices":[{"message":{"content":"[\"Dining\"]"}}]`, 200, false},
		{"wrong label count", `"choices":[{"message":{"content":"[]"}}]`, 200, true},
		{"invalid JSON answer", `"choices":[{"message":{"content":"not JSON"}}]`, 200, true},
		{"truncated", `"choices":[{"finish_reason":"length","message":{"content":"[]"}}]`, 200, true},
		{"embedded error", `"error":{"code":503}`, 200, true},
		{"HTTP error", `"error":{"code":429}`, 429, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o := NewOpenRouter("secret", "requested/model", "")
			o.client.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
				body := `{"id":"gen-test","provider":"Fireworks","model":"resolved/model","usage":{"prompt_tokens":100,"completion_tokens":40,"total_tokens":140,"cost":0.000012345,"cost_details":{"upstream_inference_cost":99},"prompt_tokens_details":{"cached_tokens":20,"cache_write_tokens":5},"completion_tokens_details":{"reasoning_tokens":30}},` + tc.payload + `}`
				return &http.Response{StatusCode: tc.status, Body: io.NopCloser(strings.NewReader(body))}, nil
			})
			_, u, err := o.ClassifyWithUsage(context.Background(), []Item{{Desc: "test"}}, nil)
			if (err != nil) != tc.wantError {
				t.Fatalf("error=%v", err)
			}
			if u == nil || u.RequestID != "gen-test" || u.Provider != "Fireworks" || u.Model != "resolved/model" || *u.InputTokens != 100 || *u.OutputTokens != 40 || *u.TotalTokens != 140 || *u.CostUSD != .000012345 || *u.ReasoningTokens != 30 || *u.CachedTokens != 20 || *u.CacheWriteTokens != 5 {
				t.Fatalf("lost or incorrect usage: %+v", u)
			}
		})
	}
}

func TestOpenRouterDistinguishesMissingAndZeroUsage(t *testing.T) {
	for _, suffix := range []string{``, `,"usage":null`, `,"usage":{}`, `,"usage":{"prompt_tokens":0,"completion_tokens":0,"total_tokens":0,"cost":0}`} {
		o := NewOpenRouter("secret", "model", "")
		o.client.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"choices":[{"message":{"content":"[\"Dining\"]"}}]` + suffix + `}`))}, nil
		})
		_, u, err := o.ClassifyWithUsage(context.Background(), []Item{{Desc: "test"}}, nil)
		if err != nil || u == nil {
			t.Fatalf("usage=%+v err=%v", u, err)
		}
		zero := strings.Contains(suffix, `"cost":0`)
		if (u.CostUSD != nil) != zero || (u.TotalTokens != nil) != zero {
			t.Fatalf("missing values treated as zero: %+v", u)
		}
		if zero && (*u.CostUSD != 0 || *u.TotalTokens != 0) {
			t.Fatal("lost reported zero")
		}
	}
}
