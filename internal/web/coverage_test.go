package web

import (
	"strings"
	"testing"

	"durooma/internal/store"
)

func TestCoverageEmptyPartialAndComplete(t *testing.T) {
	templates, err := loadTemplates()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name     string
		coverage store.CategorizationCoverage
		want     []string
	}{
		{"empty", store.CategorizationCoverage{}, []string{"No transactions yet", "import transactions"}},
		{"partial", store.CategorizationCoverage{Total: 120, Categorized: 90}, []string{
			"<strong>90</strong> of <strong>120</strong>", "75.0%", `value="90" max="120"`, "30 still uncategorized"}},
		{"complete", store.CategorizationCoverage{Total: 120, Categorized: 120}, []string{
			"100.0%", "All transactions have a category assigned."}},
		{"changed", store.CategorizationCoverage{Total: 120, Categorized: 120, CategoriesChanged: true}, []string{
			"Categories have changed", "recommend resetting"}},
		{"periods partial", store.CategorizationCoverage{Total: 120, Categorized: 120, PeriodsDecided: 30, PeriodsMoved: 4}, []string{
			"<strong>30</strong> of <strong>120</strong> periods decided", "<strong>4</strong> moved from their own month",
			"25.0%", `value="30" max="120"`, `href="/transactions?undecided_period=1">90 still undecided`}},
		{"periods complete", store.CategorizationCoverage{Total: 120, Categorized: 120, PeriodsDecided: 120}, []string{
			"Every transaction has a decided period."}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var html strings.Builder
			if err := templates.pages["settings"].ExecuteTemplate(&html, "categorization-coverage", categorizationCoverageView{CategorizationCoverage: tc.coverage}); err != nil {
				t.Fatal(err)
			}
			for _, want := range tc.want {
				if !strings.Contains(html.String(), want) {
					t.Fatalf("missing %q: %s", want, html.String())
				}
			}
			if tc.coverage.Total == 0 && strings.Contains(html.String(), "<progress") {
				t.Fatal("empty coverage must not show a misleading progress bar")
			}
		})
	}
}
