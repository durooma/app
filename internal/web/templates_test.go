package web

import (
	"io"
	"strings"
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

// TestReportDrillDownLinks checks that report figures render as links into the
// transactions view, carrying the right period/sign, and that an uncategorized
// total drills down via uncategorized=1 rather than an empty category_id.
func TestReportDrillDownLinks(t *testing.T) {
	tmpls, err := loadTemplates()
	if err != nil {
		t.Fatal(err)
	}
	cid := int64(3)
	income := []models.CategoryTotal{{CategoryID: &cid, CategoryName: "Salary", Amount: 5000}}
	expense := []models.CategoryTotal{{CategoryID: nil, CategoryName: "(uncategorized)", Amount: -800}}
	data := map[string]any{
		"Title": "T", "Nav": "month", "BaseCurrency": "CHF", "Years": []int{2024},
		"CurrentYear": 2024, "AIProvider": "gemini",
		"Year": 2024, "Month": 6, "Income": income, "Expense": expense,
		"IncomeSum": 5000.0, "ExpenseSum": -800.0, "Net": 4200.0,
	}
	var buf strings.Builder
	if err := tmpls.pages["month_deepdive"].ExecuteTemplate(&buf, "layout.html", data); err != nil {
		t.Fatalf("render month_deepdive: %v", err)
	}
	out := buf.String()
	for _, want := range []string{
		"period=2024-06&sign=income",     // Income card
		"period=2024-06&sign=expense",    // Expenses card
		"category_id=3&period=2024-06",   // categorized row
		"uncategorized=1&period=2024-06", // uncategorized row
	} {
		if !strings.Contains(out, want) {
			t.Errorf("month_deepdive missing drill-down link %q\n%s", want, out)
		}
	}
}

// TestRenderTransactionsPeriod exercises the drilled-in transactions view: the
// per-row amortized share and the period-allocated total render without error.
func TestRenderTransactionsPeriod(t *testing.T) {
	tmpls, err := loadTemplates()
	if err != nil {
		t.Fatal(err)
	}
	cid := int64(3)
	jan := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	dec := time.Date(2024, 12, 1, 0, 0, 0, 0, time.UTC)
	amortized := models.Transaction{
		ID: 9, AccountName: "Main", Institution: "UBS", Date: jan, Description: "Insurance",
		Amount: -1200, Currency: "CHF", BaseAmount: -1200, CategoryID: &cid,
		CategoryName: "Insurance", StartMonth: jan, EndMonth: dec,
	}
	data := map[string]any{
		"Title": "T", "Nav": "transactions", "BaseCurrency": "CHF", "Years": []int{2024},
		"CurrentYear": 2024, "AIProvider": "gemini",
		"Transactions": []models.Transaction{amortized},
		"Accounts":     []models.Account{}, "Institutions": []models.Institution{},
		"Categories": []models.Category{{ID: 3, Name: "Insurance"}}, "Total": -1200.0,
		"Page": 1, "HasMore": false,
		"Filter": map[string]any{"account_id": int64(0), "institution_id": int64(0),
			"category_id": int64(3), "uncategorized": false, "from": "", "to": "",
			"period": "2024-06", "sign": "", "q": ""},
		"PeriodActive": true, "Alloc": map[int64]float64{9: -100}, "AllocTotal": -100.0,
	}
	var buf strings.Builder
	if err := tmpls.pages["transactions"].ExecuteTemplate(&buf, "layout.html", data); err != nil {
		t.Fatalf("render transactions: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "this period") {
		t.Errorf("expected per-row amortized share, got:\n%s", out)
	}
	if !strings.Contains(out, "allocated to this period") {
		t.Errorf("expected period-allocated total in summary, got:\n%s", out)
	}
}

// TestRenderTxnRowPartial exercises the single-row fragment returned by the
// per-transaction auto-categorize handler, and checks the ✨ button appears
// only for still-uncategorized rows when AI is enabled.
func TestRenderTxnRowPartial(t *testing.T) {
	tmpls, err := loadTemplates()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2024, 6, 15, 0, 0, 0, 0, time.UTC)
	cats := []models.Category{{ID: 3, Name: "Groceries", Kind: "expense"}}
	uncategorized := models.Transaction{ID: 7, AccountName: "Main", Institution: "UBS",
		Date: now, Description: "Migros", Amount: -50, Currency: "CHF", BaseAmount: -50,
		StartMonth: now, EndMonth: now}

	var buf strings.Builder
	if err := tmpls.pages["transactions"].ExecuteTemplate(&buf, "txn-row",
		map[string]any{"T": uncategorized, "Cats": cats, "AIEnabled": true}); err != nil {
		t.Fatalf("render txn-row: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "/transactions/7/categorize") {
		t.Errorf("expected auto-categorize button for uncategorized row, got:\n%s", out)
	}

	// A categorized row (or AI disabled) must not offer the button.
	cid := int64(3)
	categorized := uncategorized
	categorized.CategoryID = &cid
	buf.Reset()
	if err := tmpls.pages["transactions"].ExecuteTemplate(&buf, "txn-row",
		map[string]any{"T": categorized, "Cats": cats, "AIEnabled": true}); err != nil {
		t.Fatalf("render txn-row: %v", err)
	}
	if strings.Contains(buf.String(), "/categorize") {
		t.Errorf("did not expect auto-categorize button for categorized row")
	}
}
