package store

import (
	"context"
	"testing"
	"time"

	"durooma/internal/models"
)

func TestCategoryExclusionsAcrossReports(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	defer pool.Close()
	st := New(pool)
	transferID, found, err := st.CategoryByName(ctx, "Transfer")
	if err != nil || !found {
		t.Fatalf("Transfer seed: found=%v err=%v", found, err)
	}
	transfer, err := st.GetCategory(ctx, transferID)
	if err != nil || !transfer.ExcludeFromCalculations {
		t.Fatalf("Transfer must default to excluded: %+v, %v", transfer, err)
	}
	// Inclusion belongs to the category setting, independently of its name/type.
	cat := models.Category{Name: "Internal movement test", Kind: "income", ExcludeFromCalculations: true}
	cat.ID, err = st.CreateCategory(ctx, cat)
	if err != nil {
		t.Fatal(err)
	}
	defer st.DeleteCategory(ctx, cat.ID)
	account, err := st.CreateAccount(ctx, "Exclusion test", "Main", "CHF")
	if err != nil {
		t.Fatal(err)
	}
	ordinary, found, err := st.CategoryByName(ctx, "Salary")
	if err != nil || !found {
		t.Fatalf("ordinary category: %v", err)
	}
	for i, row := range []struct {
		amount     float64
		category   *int64
		start, end time.Time
	}{
		{1200, &cat.ID, mon(2023, 12), mon(2024, 2)},
		{-600, &cat.ID, mon(2023, 12), mon(2024, 2)},
		{100, &ordinary, mon(2024, 1), mon(2024, 1)},
		{-40, &ordinary, mon(2024, 1), mon(2024, 1)},
		{10, nil, mon(2024, 1), mon(2024, 1)},
		{-5, nil, mon(2024, 1), mon(2024, 1)},
	} {
		_, err := st.InsertTransactions(ctx, []models.Transaction{{
			AccountID: account, Date: row.start, Amount: row.amount, BaseAmount: row.amount,
			Currency: "CHF", BaseCurrency: "CHF", CategoryID: row.category,
			StartMonth: row.start, EndMonth: row.end, ExternalHash: string(rune('a' + i)),
		}})
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, excluded := range []bool{true, false, true} {
		cat.ExcludeFromCalculations = excluded
		if err := st.UpdateCategory(ctx, cat); err != nil {
			t.Fatal(err)
		}
		got, err := st.GetCategory(ctx, cat.ID)
		if err != nil || got.ExcludeFromCalculations != excluded {
			t.Fatalf("category setting did not round-trip: %+v, %v", got, err)
		}
		cats, err := st.ListCategories(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range cats {
			if c.ID == cat.ID && c.ExcludeFromCalculations != excluded {
				t.Fatal("list lost category setting")
			}
		}
		for _, p := range []models.Period{{}, {Year: 2023}, {Year: 2024}, {Year: 2024, Month: 1}, {Year: 2024, Month: 2}} {
			income, expense := 110.0, -45.0
			if p.Year == 2023 || p.Month == 2 {
				income, expense = 0, 0
			}
			if !excluded {
				n := 3.0
				if p.Year == 2023 || p.IsMonth() {
					n = 1
				} else if p.Year == 2024 {
					n = 2
				}
				income += 400 * n
				expense -= 200 * n
			}
			total, err := st.Totals(ctx, p)
			if err != nil || !approxEq(total.Income, income) || !approxEq(total.Expense, expense) || !approxEq(total.Net, income+expense) {
				t.Fatalf("excluded=%v, %s: %+v err=%v, want %v/%v", excluded, p.Label(), total, err, income, expense)
			}
			breakdown, err := st.CategoryTotals(ctx, p)
			if err != nil {
				t.Fatal(err)
			}
			var sumIncome, sumExpense float64
			for _, row := range breakdown {
				if excluded && row.CategoryID != nil && *row.CategoryID == cat.ID {
					t.Fatal("excluded category in breakdown")
				}
				sumIncome += row.Income
				sumExpense += row.Expense
			}
			if !approxEq(sumIncome, income) || !approxEq(sumExpense, expense) {
				t.Fatal("breakdown does not reconcile")
			}
			if !p.IsMonth() {
				subs, err := st.SubPeriodTotals(ctx, p)
				if err != nil {
					t.Fatal(err)
				}
				sumIncome, sumExpense = 0, 0
				for _, row := range subs {
					sumIncome += row.Income
					sumExpense += row.Expense
				}
				if !approxEq(sumIncome, income) || !approxEq(sumExpense, expense) {
					t.Fatal("sub-periods do not reconcile")
				}
				cells, err := st.CategoryMatrix(ctx, p)
				if err != nil {
					t.Fatal(err)
				}
				sumIncome, sumExpense = 0, 0
				for _, row := range cells {
					if excluded && row.CategoryID != nil && *row.CategoryID == cat.ID {
						t.Fatal("excluded category in matrix")
					}
					sumIncome += row.Income
					sumExpense += row.Expense
				}
				if !approxEq(sumIncome, income) || !approxEq(sumExpense, expense) {
					t.Fatal("matrix does not reconcile")
				}
			}
		}
		all, err := st.ListTransactions(ctx, TxnFilter{})
		if err != nil || len(all) != 6 {
			t.Fatalf("excluded transactions must remain visible: %d, %v", len(all), err)
		}
		for _, txn := range all {
			if txn.CatID() == cat.ID && txn.ExcludeFromCalculations != excluded {
				t.Fatal("transaction inclusion not refreshed")
			}
		}
		for _, sign := range []string{"", "income", "expense"} {
			f := TxnFilter{IncludedOnly: true, Sign: sign}
			want := 6
			if excluded {
				want = 4
			}
			if sign != "" {
				want /= 2
			}
			rows, err := st.ListTransactions(ctx, f)
			if err != nil || len(rows) != want {
				t.Fatalf("drill-down: %d rows, want %d, %v", len(rows), want, err)
			}
			count, err := st.CountTransactions(ctx, f)
			if err != nil || count != want {
				t.Fatalf("drill-down count: %d, want %d, %v", count, want, err)
			}
		}
	}
}
