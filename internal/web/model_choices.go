package web

import "fmt"

// Reference rates in USD per million tokens, checked on OpenRouter on 2026-09-22.
// DeepSeek and GLM use Fireworks rates (the provider used in our evals);
// Gemini uses standard Google rates. Runtime ZDR routing is not pinned to these
// providers. Keep the date, source links and assumptions in settings.html in sync.
var categorizationModelChoices = []categorizationModelChoice{
	{"deepseek/deepseek-v4.1-flash", "DeepSeek V4.1 Flash (default)", 0.22, 0.66},
	{"z-ai/glm-5.3-flash", "GLM 5.3 Flash", 0.15, 0.50},
	{"google/gemini-3.1-flash-lite", "Gemini 3.1 Flash Lite", 0.25, 1.50},
}

type categorizationModelChoice struct {
	ID, Name                          string
	InputPerMillion, OutputPerMillion float64
}

func (m categorizationModelChoice) EstimatedCost() string {
	// Shared illustrative workload: GLM production eval at batch size 15 used
	// 34,663 input + 49,209 output (including reasoning) tokens for 770 rows.
	// Rounded up per 1,000: 46,000 input + 64,000 output. Other models' token
	// usage was not recorded, so these are not model-specific measurements.
	return fmt.Sprintf("US$%.2f", (46000*m.InputPerMillion+64000*m.OutputPerMillion)/1e6)
}
