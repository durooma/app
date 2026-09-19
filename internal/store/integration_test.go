package store

import (
	"context"
	"fmt"
	"math"
	"os"
	"testing"
	"time"

	"durooma/internal/db"
	"durooma/internal/models"
	"github.com/jackc/pgx/v5/pgxpool"
)

// These tests require a real Postgres. Set DUROOMA_TEST_DB to run them, e.g.
//
//	DUROOMA_TEST_DB=postgres://durooma:durooma@localhost:5433/durooma?sslmode=disable go test ./internal/store/
func testPool(t *testing.T) *pgxpool.Pool {
	url := os.Getenv("DUROOMA_TEST_DB")
	if url == "" {
		t.Skip("set DUROOMA_TEST_DB to run store integration tests")
	}
	ctx := context.Background()
	pool, err := db.Connect(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	// Clean slate for deterministic assertions.
	_, _ = pool.Exec(ctx, `TRUNCATE transactions, accounts, institutions RESTART IDENTITY CASCADE`)
	return pool
}

func approxEq(a, b float64) bool { return math.Abs(a-b) < 0.01 }

func mon(y int, m time.Month) time.Time { return time.Date(y, m, 1, 0, 0, 0, 0, time.UTC) }

func TestAmortizationAcrossMonths(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	defer pool.Close()
	st := New(pool)

	acct, err := st.CreateAccount(ctx, "UBS", "Main", "CHF")
	if err != nil {
		t.Fatal(err)
	}
	catID, _, err := st.CategoryByName(ctx, "Insurance")
	if err != nil {
		t.Fatal(err)
	}
	// Ensure a category exists to attach.
	var expenseCat int64
	if err := pool.QueryRow(ctx, `SELECT id FROM categories WHERE name='Health'`).Scan(&expenseCat); err != nil {
		t.Fatal(err)
	}
	_ = catID

	// A -300 expense spread across Q1 2024 (Jan..Mar) should contribute -100/mo.
	_, err = pool.Exec(ctx, `
		INSERT INTO transactions
		  (account_id, txn_date, description, amount, currency, base_amount, base_currency,
		   category_id, start_month, end_month, external_hash, source)
		VALUES ($1, $2, 'Annual insurance', -300, 'CHF', -300, 'CHF', $3, $4, $5, 'hash-amort', 'test')`,
		acct, mon(2024, time.January), expenseCat, mon(2024, time.January), mon(2024, time.March))
	if err != nil {
		t.Fatal(err)
	}

	year := models.Period{Year: 2024}
	months, err := st.SubPeriodTotals(ctx, year)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range months {
		want := 0.0
		if m.Period.Month >= 1 && m.Period.Month <= 3 {
			want = -100
		}
		if !approxEq(m.Expense, want) {
			t.Errorf("month %d expense = %.2f, want %.2f", m.Period.Month, m.Expense, want)
		}
	}

	totals, err := st.CategoryTotals(ctx, year)
	if err != nil {
		t.Fatal(err)
	}
	var sum float64
	for _, ct := range totals {
		sum += ct.Net
	}
	if !approxEq(sum, -300) {
		t.Errorf("year category total = %.2f, want -300", sum)
	}

	// Drilled into a single month, only that month's third is reported.
	feb, err := st.Totals(ctx, models.Period{Year: 2024, Month: 2})
	if err != nil {
		t.Fatal(err)
	}
	if !approxEq(feb.Expense, -100) {
		t.Errorf("february expense = %.2f, want -100", feb.Expense)
	}

	// The whole -300 lands in 2024 only.
	years, err := st.SubPeriodTotals(ctx, models.Period{})
	if err != nil {
		t.Fatal(err)
	}
	if len(years) != 1 || years[0].Period.Year != 2024 || !approxEq(years[0].Expense, -300) {
		t.Errorf("year totals = %+v", years)
	}
}

// TestCategoryTotalsSplitBySign covers a category holding both directions (an
// expense that was partly refunded): each side must be reported in full so the
// report's category tables add up to its headline income and expense figures.
func TestCategoryTotalsSplitBySign(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	defer pool.Close()
	st := New(pool)

	acct, err := st.CreateAccount(ctx, "UBS", "Main", "CHF")
	if err != nil {
		t.Fatal(err)
	}
	var groceries int64
	if err := pool.QueryRow(ctx, `SELECT id FROM categories WHERE name='Groceries'`).Scan(&groceries); err != nil {
		t.Fatal(err)
	}
	insert := func(desc string, amount float64, category any, hash string) {
		t.Helper()
		_, err := pool.Exec(ctx, `
			INSERT INTO transactions
			  (account_id, txn_date, description, amount, currency, base_amount, base_currency,
			   category_id, start_month, end_month, external_hash, source)
			VALUES ($1, $2, $3, $4, 'CHF', $4, 'CHF', $5, $6, $6, $7, 'test')`,
			acct, mon(2024, time.June), desc, amount, category, mon(2024, time.June), hash)
		if err != nil {
			t.Fatal(err)
		}
	}
	insert("Migros", -620, groceries, "gro-jun")
	insert("Migros refund", 90, groceries, "gro-refund-jun")
	insert("Mystery credit", 40, nil, "unk-credit-jun") // uncategorized, both signs
	insert("Unknown vendor", -10, nil, "unk-debit-jun")

	june := models.Period{Year: 2024, Month: 6}
	totals, err := st.CategoryTotals(ctx, june)
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]models.CategoryTotal{}
	var sumIncome, sumExpense float64
	for _, ct := range totals {
		byName[ct.CategoryName] = ct
		sumIncome += ct.Income
		sumExpense += ct.Expense
	}
	for _, want := range []models.CategoryTotal{
		{CategoryName: "Groceries", Income: 90, Expense: -620, Net: -530},
		{CategoryName: "(uncategorized)", Income: 40, Expense: -10, Net: 30},
	} {
		got, ok := byName[want.CategoryName]
		if !ok {
			t.Errorf("%q missing from category totals", want.CategoryName)
			continue
		}
		if !approxEq(got.Income, want.Income) || !approxEq(got.Expense, want.Expense) || !approxEq(got.Net, want.Net) {
			t.Errorf("%q = income %.2f / expense %.2f / net %.2f, want %.2f / %.2f / %.2f",
				want.CategoryName, got.Income, got.Expense, got.Net,
				want.Income, want.Expense, want.Net)
		}
	}

	// Both sides have to reconcile with the period's headline figures.
	period, err := st.Totals(ctx, june)
	if err != nil {
		t.Fatal(err)
	}
	if !approxEq(sumIncome, period.Income) || !approxEq(sumExpense, period.Expense) {
		t.Errorf("categories sum to income %.2f / expense %.2f, but the period totals are %.2f / %.2f",
			sumIncome, sumExpense, period.Income, period.Expense)
	}
}

func TestInsertDedup(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	defer pool.Close()
	st := New(pool)

	acct, err := st.CreateAccount(ctx, "UBS", "Main", "CHF")
	if err != nil {
		t.Fatal(err)
	}
	tx := models.Transaction{
		AccountID: acct, Date: mon(2024, time.June), Description: "Coffee",
		Amount: -5, Currency: "CHF", BaseAmount: -5, BaseCurrency: "CHF",
		StartMonth: mon(2024, time.June), EndMonth: mon(2024, time.June),
		ExternalHash: "dup-hash", Source: "test",
	}
	n1, err := st.InsertTransactions(ctx, []models.Transaction{tx})
	if err != nil {
		t.Fatal(err)
	}
	n2, err := st.InsertTransactions(ctx, []models.Transaction{tx})
	if err != nil {
		t.Fatal(err)
	}
	if n1 != 1 || n2 != 0 {
		t.Errorf("dedup failed: first insert %d (want 1), second %d (want 0)", n1, n2)
	}

	secondAcct, err := st.CreateAccount(ctx, "UBS", "Joint", "CHF")
	if err != nil {
		t.Fatal(err)
	}
	tx.AccountID = secondAcct
	n3, err := st.InsertTransactions(ctx, []models.Transaction{tx})
	if err != nil {
		t.Fatal(err)
	}
	if n3 != 1 {
		t.Errorf("same external hash in a different account inserted %d, want 1", n3)
	}
}

// TestCountTransactionsMatchesListing pins the property the categorization bar
// depends on: the count answers "how many match" for the same filter that
// listing pages through, so a bounded run can honestly report the remainder.
func TestCountTransactionsMatchesListing(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	defer pool.Close()
	st := New(pool)

	acct, err := st.CreateAccount(ctx, "UBS", "Main", "CHF")
	if err != nil {
		t.Fatal(err)
	}
	var groceries int64
	if err := pool.QueryRow(ctx, `SELECT id FROM categories WHERE name='Groceries'`).Scan(&groceries); err != nil {
		t.Fatal(err)
	}
	for i := range 10 {
		var cat *int64
		if i < 4 { // four are already categorized
			cat = &groceries
		}
		if _, err := pool.Exec(ctx, `
			INSERT INTO transactions
			  (account_id, txn_date, description, amount, currency, base_amount, base_currency,
			   category_id, start_month, end_month, external_hash, source)
			VALUES ($1, $2, $3, -10, 'CHF', -10, 'CHF', $4, $2, $2, $5, 'test')`,
			acct, mon(2024, time.June), fmt.Sprintf("Migros %d", i), cat,
			fmt.Sprintf("count-hash-%d", i)); err != nil {
			t.Fatal(err)
		}
	}

	f := TxnFilter{Uncategorized: true}
	total, err := st.CountTransactions(ctx, f)
	if err != nil {
		t.Fatal(err)
	}
	if total != 6 {
		t.Fatalf("count = %d, want the 6 uncategorized", total)
	}

	// A limit bounds the page but must not change what the count reports —
	// that difference is exactly the "still uncategorized" figure.
	bounded := f
	bounded.Limit = 4
	page, err := st.ListTransactions(ctx, bounded)
	if err != nil {
		t.Fatal(err)
	}
	if len(page) != 4 {
		t.Errorf("listed %d transactions, want the 4 the limit allows", len(page))
	}
	again, err := st.CountTransactions(ctx, bounded)
	if err != nil {
		t.Fatal(err)
	}
	if again != total {
		t.Errorf("count with a limit = %d, want %d: Limit must not shrink the matching set", again, total)
	}

	// The count tracks the filter, not just the table.
	all, err := st.CountTransactions(ctx, TxnFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if all != 10 {
		t.Errorf("unfiltered count = %d, want 10", all)
	}
	searched, err := st.CountTransactions(ctx, TxnFilter{Uncategorized: true, Search: "Migros 9"})
	if err != nil {
		t.Fatal(err)
	}
	if searched != 1 {
		t.Errorf("searched count = %d, want 1", searched)
	}
}
