package web

import (
	"io"
	"strings"
	"testing"
	"time"

	"durooma/internal/importer"
	"durooma/internal/models"
)

func TestLoadTemplates(t *testing.T) {
	if _, err := loadTemplates(); err != nil {
		t.Fatal(err)
	}
}

func TestImportFormSupportsMultipleFilesWithoutProviderChoice(t *testing.T) {
	tmpls, err := loadTemplates()
	if err != nil {
		t.Fatal(err)
	}
	data := map[string]any{
		"Title": "Import", "Nav": "import", "BaseCurrency": "CHF",
		"Years": []int{2026}, "CurrentYear": 2026, "AIProvider": "none",
	}
	var buf strings.Builder
	if err := tmpls.pages["import"].ExecuteTemplate(&buf, "layout.html", data); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, `name="files"`) || !strings.Contains(out, " multiple") {
		t.Fatalf("import form does not expose multi-file selection:\n%s", out)
	}
	if strings.Contains(out, `name="provider"`) {
		t.Fatalf("import form still exposes a provider choice:\n%s", out)
	}
}

// The import POST is a full page load with no browser-native feedback, so the
// form must ship the busy row the inline script reveals on submit.
func TestImportFormHasBusyIndicator(t *testing.T) {
	tmpls, err := loadTemplates()
	if err != nil {
		t.Fatal(err)
	}
	var buf strings.Builder
	data := map[string]any{
		"Title": "Import", "Nav": "import", "BaseCurrency": "CHF",
		"Years": []int{2026}, "CurrentYear": 2026, "AIProvider": "none",
	}
	if err := tmpls.pages["import"].ExecuteTemplate(&buf, "layout.html", data); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{
		"data-busy-form",  // hook the script binds to
		`class="busy"`,    // the row itself, hidden until submit
		`class="spinner"`, // animated by app.css
		`role="status"`,   // announced to screen readers
		"data-busy-text",  // swapped for the file count
		`hidden`,          // starts out of view
	} {
		if !strings.Contains(out, want) {
			t.Errorf("import form missing %q:\n%s", want, out)
		}
	}
}

// TestImportResultBanner covers the success banner, which only mentions a file
// count when more than one file was selected.
func TestImportResultBanner(t *testing.T) {
	tmpls, err := loadTemplates()
	if err != nil {
		t.Fatal(err)
	}
	render := func(imported, total int) string {
		t.Helper()
		var buf strings.Builder
		data := map[string]any{
			"Title": "Import", "Nav": "import", "BaseCurrency": "CHF",
			"Years": []int{2026}, "CurrentYear": 2026, "AIProvider": "none",
			"Result":        importer.Result{Parsed: 9, Inserted: 7, Duplicates: 2},
			"FilesImported": imported, "FilesTotal": total,
		}
		if err := tmpls.pages["import"].ExecuteTemplate(&buf, "layout.html", data); err != nil {
			t.Fatal(err)
		}
		return buf.String()
	}

	single := render(1, 1)
	if !strings.Contains(single, "<strong>7</strong> new transactions") {
		t.Errorf("single-file banner missing insert count:\n%s", single)
	}
	if strings.Contains(single, "selected files") {
		t.Errorf("single-file banner should not mention a file count:\n%s", single)
	}

	multi := render(2, 3)
	if !strings.Contains(multi, "<strong>2</strong> of <strong>3</strong> selected files") {
		t.Errorf("multi-file banner missing the 2-of-3 count:\n%s", multi)
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
		"Title": "T", "Nav": "x", "BaseCurrency": "CHF", "AIProvider": "gemini",
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

	pages := map[string]map[string]any{
		// Every scope of the consolidated report view: all time, a year, a month.
		"reports": reportData(models.Period{}, nil),
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

// reportData assembles representative data for the report view at one scope,
// mirroring what handleReports builds — including running the real breakdown so
// a change there shows up in these renders.
func reportData(p models.Period, txns []models.Transaction) map[string]any {
	cid := int64(3)
	var subs []models.PeriodTotal
	switch {
	case p.IsAll():
		subs = []models.PeriodTotal{
			{Period: models.Period{Year: 2023}, Income: 4000, Expense: -900, Net: 3100},
			{Period: models.Period{Year: 2024}, Income: 5000, Expense: -800, Net: 4200},
		}
	case p.IsYear():
		for m := 1; m <= 12; m++ {
			subs = append(subs, models.PeriodTotal{
				Period: models.Period{Year: p.Year, Month: m}, Income: 100, Expense: -40, Net: 60,
			})
		}
	}
	catTotals := []models.CategoryTotal{
		{CategoryID: &cid, CategoryName: "Salary", Kind: "income", Income: 5000, Net: 5000},
		// A mixed bucket: it belongs in both tables, not netted into one.
		{CategoryID: nil, CategoryName: "(uncategorized)", Income: 200, Expense: -1000, Net: -800},
	}
	var cells []models.PeriodCategoryCell
	for _, s := range subs {
		for _, ct := range catTotals {
			cells = append(cells, models.PeriodCategoryCell{
				Period: s.Period, CategoryID: ct.CategoryID, CategoryName: ct.CategoryName,
				Income:  ct.Income / float64(len(subs)),
				Expense: ct.Expense / float64(len(subs)),
			})
		}
	}
	income, expense := buildBreakdown(catTotals, subs, cells)

	alloc := map[int64]float64{}
	start, end := p.Bounds()
	for _, t := range txns {
		alloc[t.ID] = t.AllocatedFor(start, end)
	}
	return map[string]any{
		"Title": "T", "Nav": "reports", "BaseCurrency": "CHF", "AIProvider": "gemini",
		"Period": p, "Crumbs": crumbs(p),
		"Totals":  models.PeriodTotal{Period: p, Income: 5000, Expense: -800, Net: 4200},
		"Income":  income,
		"Expense": expense,
		"Subs":    subs,

		"Transactions": txns, "Alloc": alloc, "TxnsTruncated": false,
	}
}

// TestBreakdownReconcilesWithTotals pins the property the headline cards depend
// on: the category tables count every allocation, so each side adds up to the
// period's total. A category with both income and expenses (a refunded expense,
// a mixed uncategorized bucket) has to appear in both tables rather than being
// netted into whichever side its balance happens to fall on.
func TestBreakdownReconcilesWithTotals(t *testing.T) {
	cid := int64(8)
	jun := models.Period{Year: 2025, Month: 6}
	subs := []models.PeriodTotal{{Period: jun, Income: 9590, Expense: -3730, Net: 5860}}
	totals := []models.CategoryTotal{
		{CategoryID: nil, CategoryName: "(uncategorized)", Income: 1500, Expense: -310, Net: 1190},
		{CategoryID: &cid, CategoryName: "Groceries", Income: 90, Expense: -620, Net: -530},
		{CategoryName: "Salary", Income: 8000, Net: 8000},
		{CategoryName: "Housing", Expense: -2800, Net: -2800},
	}
	var cells []models.PeriodCategoryCell
	for _, ct := range totals {
		cells = append(cells, models.PeriodCategoryCell{Period: jun, CategoryID: ct.CategoryID,
			CategoryName: ct.CategoryName, Income: ct.Income, Expense: ct.Expense})
	}

	income, expense := buildBreakdown(totals, subs, cells)

	var gotIncome, gotExpense float64
	for _, r := range income {
		gotIncome += r.Total
	}
	for _, r := range expense {
		gotExpense += r.Total
	}
	if gotIncome != 9590 {
		t.Errorf("income rows sum to %.2f, want the period's 9590 income", gotIncome)
	}
	if gotExpense != -3730 {
		t.Errorf("expense rows sum to %.2f, want the period's -3730 expenses", gotExpense)
	}

	// The mixed categories land on both sides, each with only its own half.
	find := func(rows []breakdownRow, name string) (breakdownRow, bool) {
		for _, r := range rows {
			if r.Category == name {
				return r, true
			}
		}
		return breakdownRow{}, false
	}
	for _, tc := range []struct {
		rows  []breakdownRow
		name  string
		total float64
	}{
		{income, "(uncategorized)", 1500},
		{expense, "(uncategorized)", -310},
		{income, "Groceries", 90},
		{expense, "Groceries", -620},
	} {
		row, ok := find(tc.rows, tc.name)
		if !ok {
			t.Errorf("%q missing from the breakdown side holding %.2f", tc.name, tc.total)
			continue
		}
		if row.Total != tc.total {
			t.Errorf("%q total = %.2f, want %.2f", tc.name, row.Total, tc.total)
		}
		if len(row.Cells) != 1 || row.Cells[0].Amount != tc.total {
			t.Errorf("%q cells = %+v, want the same %.2f split into June", tc.name, row.Cells, tc.total)
		}
	}

	// A single-sided category stays on its own side only.
	if _, ok := find(expense, "Salary"); ok {
		t.Error("Salary has no expenses but appears in the expense table")
	}
	if _, ok := find(income, "Housing"); ok {
		t.Error("Housing has no income but appears in the income table")
	}

	// Ordering: biggest first on both sides.
	if income[0].Category != "Salary" || expense[0].Category != "Housing" {
		t.Errorf("rows not sorted biggest-first: income %q, expense %q",
			income[0].Category, expense[0].Category)
	}
}

// TestReportDrillDown walks the three scopes of the consolidated report view and
// checks each one offers the right next step: all time drills into a year, a
// year into a month, and a month lists its transactions. Aggregate figures must
// link into the transactions view carrying the matching period/sign, with an
// uncategorized total using uncategorized=1 rather than an empty category_id.
func TestReportDrillDown(t *testing.T) {
	tmpls, err := loadTemplates()
	if err != nil {
		t.Fatal(err)
	}
	jan := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	dec := time.Date(2024, 12, 1, 0, 0, 0, 0, time.UTC)
	cid := int64(3)
	amortized := models.Transaction{
		ID: 9, AccountName: "Main", Institution: "UBS", Date: jan, Description: "Insurance",
		Amount: -1200, Currency: "CHF", BaseAmount: -1200, CategoryID: &cid,
		CategoryName: "Insurance", StartMonth: jan, EndMonth: dec,
	}

	render := func(p models.Period, txns []models.Transaction) string {
		t.Helper()
		var buf strings.Builder
		if err := tmpls.pages["reports"].ExecuteTemplate(&buf, "layout.html", reportData(p, txns)); err != nil {
			t.Fatalf("render reports at %s: %v", p.Label(), err)
		}
		return buf.String()
	}

	cases := []struct {
		scope   models.Period
		txns    []models.Transaction
		want    []string
		notWant []string
	}{{
		scope: models.Period{},
		want: []string{
			`href="/reports?year=2024"`,                // drill into a year
			"period=&sign=income",                      // all-time income card: no period filter
			"period=2024&sign=expense",                 // the 2024 row's expenses
			"category_id=3&period=2023&sign=income",    // a category in one year
			"uncategorized=1&period=2024&sign=expense", // uncategorized drills down by flag
			`<th class="num">2024</th>`,                // breakdown has a column per year
		},
		notWant: []string{"<h2>Transactions</h2>"},
	}, {
		scope: models.Period{Year: 2024},
		want: []string{
			`href="/reports?month=6&amp;year=2024"`,    // drill into a month
			"period=2024&sign=income",                  // the year's income card
			"period=2024-06&sign=expense",              // June's expenses
			"category_id=3&period=2024-06&sign=income", // a category in June
			`<th class="num">Jun</th>`,                 // breakdown has a column per month
		},
		notWant: []string{"<h2>Transactions</h2>"},
	}, {
		scope: models.Period{Year: 2024, Month: 6},
		txns:  []models.Transaction{amortized},
		want: []string{
			"period=2024-06&sign=income",                  // the month's income card
			"category_id=3&period=2024-06&sign=income",    // a category in the month
			"uncategorized=1&period=2024-06&sign=expense", // uncategorized in the month
			"<h2>Transactions</h2>",                       // the deepest level lists them
			"Insurance",
			"-100.00 this month", // the amortized share of a -1200/12 transaction
		},
		notWant: []string{"<h2>By month</h2>"}, // nothing left to drill into
	}}

	for _, tc := range cases {
		out := render(tc.scope, tc.txns)
		for _, want := range tc.want {
			if !strings.Contains(out, want) {
				t.Errorf("report at %s missing %q\n%s", tc.scope.Label(), want, out)
			}
		}
		for _, notWant := range tc.notWant {
			if strings.Contains(out, notWant) {
				t.Errorf("report at %s should not contain %q\n%s", tc.scope.Label(), notWant, out)
			}
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
