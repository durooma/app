package store

import (
	"bytes"
	"context"
	"durooma/internal/models"
	"durooma/internal/secrets"
	"testing"
	"time"
)

func TestResetCategorizationClearsAssignmentsAndRetries(t *testing.T) {
	pool := testPool(t)
	defer pool.Close()
	ctx := context.Background()
	vault, _ := secrets.New(bytes.Repeat([]byte{7}, 32))
	st := NewWithVault(pool, vault)
	const owner = "reset-test"
	defer pool.Exec(ctx, `DELETE FROM user_settings WHERE user_id=$1`, owner)
	account, err := st.CreateAccount(ctx, "reset test", "reset", "CHF")
	if err != nil {
		t.Fatal(err)
	}
	category, err := st.CreateCategory(ctx, models.Category{Name: "Reset test category"})
	if err != nil {
		t.Fatal(err)
	}
	defer st.DeleteCategory(ctx, category)
	_, err = st.InsertTransactions(ctx, []models.Transaction{
		{AccountID: account, Date: mon(2026, time.January), StartMonth: mon(2026, time.January), EndMonth: mon(2026, time.January), Description: "manual", Currency: "CHF", BaseCurrency: "CHF", ExternalHash: "reset-manual", CategoryID: &category},
		{AccountID: account, Date: mon(2026, time.January), StartMonth: mon(2026, time.January), EndMonth: mon(2026, time.January), Description: "retry", Currency: "CHF", BaseCurrency: "CHF", ExternalHash: "reset-retry"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `UPDATE transactions SET categorize_attempts=5,categorize_after=now()+interval '1 day'; UPDATE categorization_state SET run_revision=NULL`); err != nil {
		t.Fatal(err)
	}
	if err = st.ResetCategorization(ctx, owner); err == nil {
		t.Fatal("reset accepted without key")
	}
	coverage, err := st.CategorizationCoverage(ctx)
	if err != nil || coverage.Categorized != 1 {
		t.Fatalf("failed reset changed assignments: %+v %v", coverage, err)
	}
	if err = st.BeginCategorization(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `UPDATE categories SET description='updated definition' WHERE id=$1`, category); err != nil {
		t.Fatal(err)
	}
	if err = st.BeginCategorization(ctx); err != nil {
		t.Fatal(err)
	}
	coverage, err = st.CategorizationCoverage(ctx)
	if err != nil || !coverage.CategoriesChanged {
		t.Fatal("incremental run hid stale assignments", err)
	}
	settings := models.DefaultAISettings()
	settings.Concurrency = 2
	if err = st.SaveAISettings(ctx, owner, settings, "sk-or-test-reset-credential", false); err != nil {
		t.Fatal(err)
	}
	if err = st.ResetCategorization(ctx, owner); err != nil {
		t.Fatal(err)
	}
	coverage, err = st.CategorizationCoverage(ctx)
	if err != nil || coverage.Total != 2 || coverage.Categorized != 0 || coverage.CategoriesChanged {
		t.Fatalf("reset: %+v %v", coverage, err)
	}
	due, err := st.DueCategorization(ctx, 10)
	if err != nil || len(due) != 2 {
		t.Fatal("retry queue not reset", err)
	}
	var attempts int
	if err = pool.QueryRow(ctx, `SELECT sum(categorize_attempts) FROM transactions`).Scan(&attempts); err != nil || attempts != 0 {
		t.Fatal("attempts not reset")
	}
	settings, err = st.AISettings(ctx, owner)
	if err != nil || !settings.Background || !settings.HasKey || settings.Concurrency != 2 {
		t.Fatal("restart preferences incorrect")
	}
	if _, err = st.GetCategory(ctx, category); err != nil {
		t.Fatal("category definition removed")
	}
	if err = st.BeginCategorization(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `UPDATE categories SET name=name WHERE id=$1`, category); err != nil {
		t.Fatal(err)
	}
	coverage, _ = st.CategorizationCoverage(ctx)
	if coverage.CategoriesChanged {
		t.Fatal("no-op edit marked stale")
	}
	if err = st.DeleteCategory(ctx, category); err != nil {
		t.Fatal(err)
	}
	coverage, _ = st.CategorizationCoverage(ctx)
	if !coverage.CategoriesChanged {
		t.Fatal("deletion not tracked")
	}
}
