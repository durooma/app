package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// OpenRouter uses the synchronous Chat Completions API. Its immutable client
// may be shared by concurrent eval workers. The caller owns the deadline.
type OpenRouter struct {
	apiKey, model, prompt string
	client                *http.Client
	routing               *OpenRouterRouting
}

// OpenRouterRouting is sent on every request, including all retry attempts.
// ZDR is a routing constraint, not a suffix added to the model ID.
type OpenRouterRouting struct {
	ZDR            bool     `json:"zdr,omitempty"`
	DataCollection string   `json:"data_collection,omitempty"`
	Only           []string `json:"only,omitempty"`
}

func NewOpenRouter(key, model, prompt string) *OpenRouter {
	return NewOpenRouterWithRouting(key, model, prompt, nil)
}
func NewOpenRouterWithRouting(key, model, prompt string, routing *OpenRouterRouting) *OpenRouter {
	o := &OpenRouter{apiKey: key, model: model, prompt: prompt, client: newEvalHTTPClient()}
	if routing != nil {
		copy := *routing
		copy.Only = append([]string(nil), routing.Only...)
		o.routing = &copy
	}
	return o
}
func (o *OpenRouter) Name() string { return "openrouter/" + o.model }
func (o *OpenRouter) Classify(ctx context.Context, items []Item, categories []CategoryDef) ([]string, error) {
	names, _, err := o.ClassifyWithUsage(ctx, items, categories)
	return names, err
}

func (o *OpenRouter) ClassifyWithUsage(ctx context.Context, items []Item, categories []CategoryDef) ([]string, *Usage, error) {
	var usage *Usage
	if o.apiKey == "" || o.model == "" {
		return nil, usage, fmt.Errorf("openrouter: key and model are required")
	}
	prompt, err := RenderPrompt(o.prompt, items, categories)
	if err != nil {
		return nil, usage, fmt.Errorf("openrouter: prompt: %w", err)
	}
	payload := map[string]any{"model": o.model, "stream": false, "messages": []map[string]string{{"role": "user", "content": prompt}}}
	if o.routing != nil {
		payload["provider"] = o.routing
	}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://openrouter.ai/api/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, usage, err
	}
	req.Header.Set("Authorization", "Bearer "+o.apiKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := o.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, usage, ctx.Err()
		}
		return nil, usage, &NetworkError{Kind: "openrouter: request failed"}
	}
	defer resp.Body.Close()
	var parsed struct {
		ID       string `json:"id"`
		Provider string `json:"provider"`
		Model    string `json:"model"`
		Usage    *struct {
			PromptTokens     *int64   `json:"prompt_tokens"`
			CompletionTokens *int64   `json:"completion_tokens"`
			TotalTokens      *int64   `json:"total_tokens"`
			Cost             *float64 `json:"cost"`
			PromptDetails    struct {
				CachedTokens     *int64 `json:"cached_tokens"`
				CacheWriteTokens *int64 `json:"cache_write_tokens"`
			} `json:"prompt_tokens_details"`
			CompletionDetails struct {
				ReasoningTokens *int64 `json:"reasoning_tokens"`
			} `json:"completion_tokens_details"`
		} `json:"usage"`
		Error   json.RawMessage `json:"error"`
		Choices []struct {
			FinishReason string `json:"finish_reason"`
			Message      struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	decodeErr := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&parsed)
	if decodeErr == nil {
		usage = &Usage{RequestID: parsed.ID, Provider: parsed.Provider, Model: parsed.Model}
		if u := parsed.Usage; u != nil {
			usage.InputTokens, usage.OutputTokens, usage.TotalTokens = u.PromptTokens, u.CompletionTokens, u.TotalTokens
			usage.CostUSD = u.Cost
			usage.ReasoningTokens = u.CompletionDetails.ReasoningTokens
			usage.CachedTokens, usage.CacheWriteTokens = u.PromptDetails.CachedTokens, u.PromptDetails.CacheWriteTokens
		}
	}
	if resp.StatusCode != http.StatusOK {
		return nil, usage, &APIError{Status: resp.StatusCode, RetryAfter: retryAfter(resp.Header.Get("Retry-After"))}
	}
	if decodeErr != nil {
		return nil, usage, &NetworkError{Kind: "openrouter: invalid API response JSON"}
	}
	// OpenRouter may return a provider error inside a successful HTTP response.
	if len(parsed.Error) > 0 && string(parsed.Error) != "null" {
		var detail struct {
			Code int `json:"code"`
		}
		if json.Unmarshal(parsed.Error, &detail) != nil || detail.Code == 0 {
			detail.Code = http.StatusBadGateway
		}
		return nil, usage, &APIError{Status: detail.Code, RetryAfter: retryAfter(resp.Header.Get("Retry-After"))}
	}
	if len(parsed.Choices) == 0 {
		return nil, usage, fmt.Errorf("openrouter: empty response")
	}
	choice := parsed.Choices[0]
	if choice.FinishReason != "" && choice.FinishReason != "stop" {
		return nil, usage, fmt.Errorf("openrouter: incomplete response (%s)", choice.FinishReason)
	}
	value := strings.TrimSpace(choice.Message.Content)
	value = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(value, "```json"), "```"), "```"))
	var names []string
	if err := json.Unmarshal([]byte(value), &names); err != nil {
		return nil, usage, fmt.Errorf("openrouter: could not parse JSON category array")
	}
	if len(names) != len(items) {
		return nil, usage, fmt.Errorf("openrouter: expected %d categories, got %d", len(items), len(names))
	}
	return names, usage, nil
}
