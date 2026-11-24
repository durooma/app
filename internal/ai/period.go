package ai

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// PeriodItem is a single transaction whose reporting period is to be decided.
// The date anchors the relative answer; the category, when known, is a strong
// hint (an insurance premium is far more likely to be yearly than groceries).
type PeriodItem struct {
	Date     time.Time
	Desc     string
	Amount   float64
	Category string
}

// PeriodProvider is implemented by providers that can also suggest periods.
// It returns one label from PeriodLabels per item, in order.
type PeriodProvider interface {
	ClassifyPeriods(ctx context.Context, items []PeriodItem) ([]string, error)
}

// PeriodLabels is the closed vocabulary the model answers with: a calendar
// month, quarter or year relative to the transaction date. Asking for a
// relative label rather than a literal "2026-Q3" keeps answers anchored to the
// transaction and trivially checkable.
var PeriodLabels = []string{
	"month", "quarter", "year",
	"previous_month", "previous_quarter", "previous_year",
	"next_month", "next_quarter", "next_year",
}

// PeriodWindow resolves a label to the inclusive first-of-month window it
// names around date. ok is false for anything outside PeriodLabels.
func PeriodWindow(date time.Time, label string) (start, end time.Time, ok bool) {
	label = strings.ToLower(strings.TrimSpace(label))
	shift := 0
	if rest, found := strings.CutPrefix(label, "previous_"); found {
		label, shift = rest, -1
	} else if rest, found := strings.CutPrefix(label, "next_"); found {
		label, shift = rest, 1
	}
	month := time.Date(date.Year(), date.Month(), 1, 0, 0, 0, 0, time.UTC)
	switch label {
	case "month":
		start = month.AddDate(0, shift, 0)
		return start, start, true
	case "quarter":
		start = month.AddDate(0, -(int(month.Month())-1)%3+3*shift, 0)
		return start, start.AddDate(0, 2, 0), true
	case "year":
		start = time.Date(date.Year()+shift, time.January, 1, 0, 0, 0, 0, time.UTC)
		return start, start.AddDate(0, 11, 0), true
	}
	return time.Time{}, time.Time{}, false
}

// PeriodPromptTemplate asks for one relative period label per transaction.
const PeriodPromptTemplate = `You are a financial assistant that decides which budgeting period a bank transaction's amount belongs to.

Almost every transaction belongs to the calendar month it occurred in: answer "month".
Choose something else only when the description, amount and category clearly indicate it:
- "year": an annual premium, subscription, membership, licence, tax or fee covering the current calendar year
- "quarter": a bill covering the current calendar quarter
- "previous_month", "previous_quarter", "previous_year": a bill paid after the period it covers, e.g. electricity for last quarter
- "next_month", "next_quarter", "next_year": a payment made in advance, e.g. next year's insurance premium paid in December, or a salary for the following month
Periods are calendar months, quarters and years relative to the transaction date. When unsure, answer "month".

Transactions:
{{.Transactions}}
Task:
Return ONLY a JSON array of strings, one per transaction, in the same order,
each one of: {{.Labels}}. Example: ["month","year"]`

// RenderPeriodPrompt builds the period prompt for a batch of transactions.
func RenderPeriodPrompt(items []PeriodItem) string {
	var batch strings.Builder
	for i, it := range items {
		fmt.Fprintf(&batch, "[ID: %d] Date: %s  Transaction: %q  Amount: %.2f", i, it.Date.Format("2006-01-02"), it.Desc, it.Amount)
		if it.Category != "" {
			fmt.Fprintf(&batch, "  Category: %q", it.Category)
		}
		batch.WriteByte('\n')
	}
	labels := make([]string, len(PeriodLabels))
	for i, l := range PeriodLabels {
		labels[i] = `"` + l + `"`
	}
	r := strings.NewReplacer("{{.Transactions}}", batch.String(), "{{.Labels}}", strings.Join(labels, ", "))
	return r.Replace(PeriodPromptTemplate)
}
