package eval

import "fmt"

// UsageTotals sums API-reported usage for all attempts, independent of scoring.
// Missing reports are not inferred to be free, including timeouts and 429s.
type UsageTotals struct {
	Attempts              int      `json:"attempts"`
	TokenReportedAttempts int      `json:"token_reported_attempts"`
	CostReportedAttempts  int      `json:"cost_reported_attempts"`
	InputTokens           *int64   `json:"input_tokens"`
	OutputTokens          *int64   `json:"output_tokens"`
	TotalTokens           *int64   `json:"total_tokens"`
	ReasoningTokens       *int64   `json:"reasoning_tokens"`
	CachedTokens          *int64   `json:"cached_tokens"`
	CacheWriteTokens      *int64   `json:"cache_write_tokens"`
	CostUSD               *float64 `json:"cost_usd"`
}

func addReported[T int64 | float64](total **T, value *T) {
	if value == nil {
		return
	}
	if *total == nil {
		*total = new(T)
	}
	**total += *value
}

func (u *UsageTotals) addBatches(batches []Batch) {
	for _, b := range batches {
		for _, a := range b.Attempts {
			u.Attempts++
			v := a.Usage
			if v == nil {
				continue
			}
			if v.InputTokens != nil && v.OutputTokens != nil && v.TotalTokens != nil {
				u.TokenReportedAttempts++
			}
			if v.CostUSD != nil {
				u.CostReportedAttempts++
			}
			addReported(&u.InputTokens, v.InputTokens)
			addReported(&u.OutputTokens, v.OutputTokens)
			addReported(&u.TotalTokens, v.TotalTokens)
			addReported(&u.ReasoningTokens, v.ReasoningTokens)
			addReported(&u.CachedTokens, v.CachedTokens)
			addReported(&u.CacheWriteTokens, v.CacheWriteTokens)
			addReported(&u.CostUSD, v.CostUSD)
		}
	}
}

func tokenText(v *int64) string {
	if v == nil {
		return "n/a"
	}
	return fmt.Sprint(*v)
}

func coverageText(reported, attempts int) string {
	if attempts == 0 || reported == 0 {
		return fmt.Sprintf("unavailable; %d/%d attempts reported", reported, attempts)
	}
	if reported < attempts {
		return fmt.Sprintf("incomplete; %d/%d attempts reported", reported, attempts)
	}
	return fmt.Sprintf("%d/%d attempts reported", reported, attempts)
}

// String keeps partial totals visibly distinct from the cost of the whole run.
func (u UsageTotals) String() string {
	cost := "n/a"
	if u.CostUSD != nil {
		cost = fmt.Sprintf("$%.8f", *u.CostUSD)
	}
	s := fmt.Sprintf("tokens: input=%s, output=%s, total=%s (%s); reported cost: %s USD (%s)",
		tokenText(u.InputTokens), tokenText(u.OutputTokens), tokenText(u.TotalTokens), coverageText(u.TokenReportedAttempts, u.Attempts), cost, coverageText(u.CostReportedAttempts, u.Attempts))
	if u.ReasoningTokens != nil {
		s += fmt.Sprintf("; reported reasoning=%d (included in output)", *u.ReasoningTokens)
	}
	if u.CachedTokens != nil {
		s += fmt.Sprintf("; reported cached input=%d", *u.CachedTokens)
	}
	return s
}
