package ai

import "context"

// Usage is per attempt, never mutable provider state. Nil numeric fields mean
// unavailable, whereas a pointer to zero is an explicitly reported zero.
type Usage struct {
	RequestID        string   `json:"request_id,omitempty"`
	Provider         string   `json:"provider,omitempty"`
	Model            string   `json:"model,omitempty"`
	InputTokens      *int64   `json:"input_tokens"`
	OutputTokens     *int64   `json:"output_tokens"`
	TotalTokens      *int64   `json:"total_tokens"`
	ReasoningTokens  *int64   `json:"reasoning_tokens"`
	CachedTokens     *int64   `json:"cached_tokens"`
	CacheWriteTokens *int64   `json:"cache_write_tokens"`
	CostUSD          *float64 `json:"cost_usd"`
}

// UsageProvider optionally extends Provider without changing production callers.
// Usage may be returned alongside an error (e.g. a billed but invalid answer).
type UsageProvider interface {
	ClassifyWithUsage(context.Context, []Item, []CategoryDef) ([]string, *Usage, error)
}
