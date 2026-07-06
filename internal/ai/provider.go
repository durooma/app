package ai

import (
	"context"
	"fmt"
)

// NewProvider selects an LLM provider from configuration. Add cases here to
// support OpenAI, Claude, or a local model without touching the rest of the app.
func NewProvider(name, apiKey, model string) (Provider, error) {
	switch name {
	case "gemini", "":
		return NewGemini(apiKey, model), nil
	case "none", "disabled":
		return Noop{}, nil
	default:
		return nil, fmt.Errorf("unknown AI provider %q (supported: gemini, none)", name)
	}
}

// Noop is a provider that classifies nothing — used when AI is disabled.
type Noop struct{}

func (Noop) Name() string { return "disabled" }

func (Noop) Classify(context.Context, []Item, []CategoryDef) ([]string, error) {
	return nil, fmt.Errorf("AI categorization is disabled (set AI_PROVIDER and AI_API_KEY)")
}
