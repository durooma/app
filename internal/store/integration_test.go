package store

import (
	"context"
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

	months, err := st.MonthTotalsForYear(ctx, 2024)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range months {
		want := 0.0
		if m.Month >= 1 && m.Month <= 3 {
			want = -100
		}
		if !approxEq(m.Expense, want) {
			t.Errorf("month %d expense = %.2f, want %.2f", m.Month, m.Expense, want)
		}
	}

	totals, err := st.CategoryTotalsForYear(ctx, 2024)
	if err != nil {
		t.Fatal(err)
	}
	var sum float64
	for _, ct := range totals {
		sum += ct.Amount
	}
	if !approxEq(sum, -300) {
		t.Errorf("year category total = %.2f, want -300", sum)
	}

	// The whole -300 lands in 2024 only.
	years, err := st.YearTotals(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(years) != 1 || years[0].Year != 2024 || !approxEq(years[0].Expense, -300) {
		t.Errorf("year totals = %+v", years)
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
