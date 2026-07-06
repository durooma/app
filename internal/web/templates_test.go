package web

import (
	"io"
	"testing"
	"time"

	"durooma/internal/models"
)

func TestLoadTemplates(t *testing.T) {
	if _, err := loadTemplates(); err != nil {
		t.Fatal(err)
	}
}

// TestRenderAllPages exercises every page template with representative data so
// that field/method/func typos surface as test failures, not runtime 500s.
func TestRenderAllPages(t *testing.T) {
	tmpls, err := loadTemplates()
	if err != nil {
		t.Fatal(err)
	}

	cid := int64(3)
	now := time.Date(2024, 6, 15, 0, 0, 0, 0, time.UTC)
	common := map[string]any{
		"Title": "T", "Nav": "x", "BaseCurrency": "CHF",
		"Years": []int{2024, 2023}, "CurrentYear": 2024, "AIProvider": "gemini",
	}
	merge := func(extra map[string]any) map[string]any {
		m := map[string]any{}
		for k, v := range common {
			m[k] = v
		}
		for k, v := range extra {
			m[k] = v
		}
		return m
	}

	txns := []models.Transaction{{
		ID: 1, AccountName: "Main", Institution: "UBS", Date: now,
		Description: "Migros", Amount: -50, Currency: "CHF", BaseAmount: -50,
		CategoryID: &cid, CategoryName: "Groceries", StartMonth: now, EndMonth: now,
	}}
	cats := []models.Category{{ID: 3, Name: "Groceries", Kind: "expense"}}
	catTotals := []models.CategoryTotal{
		{CategoryID: &cid, CategoryName: "Salary", Amount: 5000},
		{CategoryID: &cid, CategoryName: "Groceries", Amount: -800},
	}
	months := make([]models.MonthTotal, 12)
	for i := range months {
		months[i] = models.MonthTotal{Month: i + 1, Income: 100, Expense: -40, Net: 60}
	}

	pages := map[string]map[string]any{
		"year_overview":  {"Year": 2024, "Rows": []*matrixRow{{Category: "Groceries", Total: -800}}, "Months": months},
		"year_deepdive":  {"Year": 2024, "Income": catTotals[:1], "Expense": catTotals[1:], "IncomeSum": 5000.0, "ExpenseSum": -800.0, "Net": 4200.0, "Months": months},
		"month_deepdive": {"Year": 2024, "Month": 6, "Income": catTotals[:1], "Expense": catTotals[1:], "IncomeSum": 5000.0, "ExpenseSum": -800.0, "Net": 4200.0},
		"multi_year": {"Years": []int{2024, 2023}, "YearTotals": []models.YearTotal{{Year: 2024, Income: 5000, Expense: -800, Net: 4200}},
			"Rows": []*yearMatrixRow{{Category: "Groceries", ByYear: map[int]float64{2024: -800}, Total: -800}}},
		"transactions": {"Transactions": txns, "Accounts": []models.Account{{ID: 1, Name: "Main", InstitutionName: "UBS", Currency: "CHF"}},
			"Institutions": []models.Institution{{ID: 1, Name: "UBS"}}, "Categories": cats, "Total": -50.0,
			"Page": 1, "HasMore": false, "Filter": map[string]any{"account_id": int64(0), "institution_id": int64(0), "category_id": int64(0), "uncategorized": false, "from": "", "to": "", "q": ""}},
		"categories": {"Categories": cats},
		"rules":      {"Rules": []models.Rule{{ID: 1, Pattern: "MIGROS", CategoryName: "Groceries", Priority: 1}}, "Categories": cats},
		"accounts":   {"Accounts": []models.Account{{ID: 1, Name: "Main", InstitutionName: "UBS", Currency: "CHF"}}},
		"import":     {},
	}

	for page, extra := range pages {
		if _, ok := tmpls.pages[page]; !ok {
			t.Errorf("template %q not loaded", page)
			continue
		}
		if err := tmpls.pages[page].ExecuteTemplate(io.Discard, "layout.html", merge(extra)); err != nil {
			t.Errorf("render %q: %v", page, err)
		}
	}
}
