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
	} {
		t.Run(tc.name, func(t *testing.T) {
			var html strings.Builder
			if err := templates.pages["settings"].ExecuteTemplate(&html, "categorization-coverage", tc.coverage); err != nil {
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
