package store

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"durooma/internal/models"
)

func TestCategorizationQueueRetryAndManualEdit(t *testing.T) {
	pool := testPool(t)
	defer pool.Close()
	ctx := context.Background()
	st := New(pool)
	checkCoverage := func(total, categorized int) {
		t.Helper()
		coverage, err := st.CategorizationCoverage(ctx)
		if err != nil || coverage.Total != total || coverage.Categorized != categorized || coverage.Remaining() != total-categorized {
			t.Fatalf("coverage = %+v, err = %v; want %d of %d", coverage, err, categorized, total)
		}
		percent := 0.0
		if total > 0 {
			percent = 100 * float64(categorized) / float64(total)
		}
		if !approxEq(coverage.Percent(), percent) {
			t.Fatalf("percentage = %v, want %v", coverage.Percent(), percent)
		}
	}
	checkCoverage(0, 0)
	account, err := st.CreateAccount(ctx, "queue test", "account", "CHF")
	if err != nil {
		t.Fatal(err)
	}
	var txns []models.Transaction
	for i := 0; i < 3; i++ {
		txns = append(txns, models.Transaction{AccountID: account, Date: mon(2024, time.June),
			StartMonth: mon(2024, time.June), EndMonth: mon(2024, time.June), Description: "test",
			Currency: "CHF", BaseCurrency: "CHF", ExternalHash: fmt.Sprintf("queue-%d", i)})
	}
	if _, err := st.InsertTransactions(ctx, txns); err != nil {
		t.Fatal(err)
	}
	checkCoverage(3, 0)
	pending, err := st.DueCategorization(ctx, 2)
	if err != nil || len(pending) != 2 {
		t.Fatalf("pending=%v err=%v", pending, err)
	}
	ids := []int64{pending[0].ID, pending[1].ID}
	if err := st.DeferCategorization(ctx, ids); err != nil {
		t.Fatal(err)
	}
	// A new store instance sees the persisted retry schedule, as on restart.
	restarted := New(pool)
	due, err := restarted.DueCategorization(ctx, 10)
	if err != nil || len(due) != 1 {
		t.Fatalf("deferred rows block other work: %v %v", due, err)
	}
	cats, err := st.ListCategories(ctx)
	if err != nil || len(cats) < 2 {
		t.Fatalf("categories: %v", err)
	}
	manual, ai := cats[0].ID, cats[1].ID
	if err := st.UpdateTransactionCategory(ctx, ids[0], &manual); err != nil {
		t.Fatal(err)
	}
	if err := st.SetUncategorizedCategory(ctx, ids[0], &ai); err != nil {
		t.Fatal(err)
	}
	checkCoverage(3, 1)
	got, err := st.GetTransaction(ctx, ids[0])
	if err != nil || got.CategoryID == nil || *got.CategoryID != manual {
		t.Fatalf("manual edit overwritten: %+v %v", got, err)
	}
	fresh, err := st.PendingTransactions(ctx, ids)
	if err != nil || len(fresh) != 1 || fresh[0].ID != ids[1] {
		t.Fatalf("stale snapshot: %v %v", fresh, err)
	}
	// Make the remaining retry due without waiting on wall-clock time.
	if _, err := pool.Exec(ctx, `UPDATE transactions SET categorize_after = now() - interval '1 second' WHERE id = $1`, ids[1]); err != nil {
		t.Fatal(err)
	}
	due, err = restarted.DueCategorization(ctx, 10)
	if err != nil || len(due) != 2 {
		t.Fatalf("retry did not reappear: %v %v", due, err)
	}
	for _, txn := range due {
		if err := st.SetUncategorizedCategory(ctx, txn.ID, &ai); err != nil {
			t.Fatal(err)
		}
	}
	checkCoverage(3, 3)
	if err := st.UpdateTransactionCategory(ctx, ids[0], nil); err != nil {
		t.Fatal(err)
	}
	checkCoverage(3, 2)
}

func TestSharedRequestBudgetPersistsAndSerializes(t *testing.T) {
	pool := testPool(t)
	defer pool.Close()
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `UPDATE ai_request_budget SET next_request_at = now(), failures = 0`); err != nil {
		t.Fatal(err)
	}
	st := New(pool)
	var wg sync.WaitGroup
	waits := make(chan time.Duration, 8)
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			wait, err := st.ReserveAIRequest(ctx, time.Minute)
			waits <- wait
			errs <- err
		}()
	}
	wg.Wait()
	close(waits)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	immediate := 0
	for wait := range waits {
		if wait == 0 {
			immediate++
		}
	}
	if immediate != 1 {
		t.Fatalf("%d concurrent requests admitted, want 1", immediate)
	}
	if err := st.FinishAIRequest(ctx, true, time.Hour); err != nil {
		t.Fatal(err)
	}
	restarted := New(pool)
	wait, err := restarted.ReserveAIRequest(ctx, time.Minute)
	if err != nil || wait < 59*time.Minute {
		t.Fatalf("cooldown lost across restart: %v %v", wait, err)
	}
	// Success clears error backoff but must not erase an already reserved delay.
	if err := st.FinishAIRequest(ctx, false, 0); err != nil {
		t.Fatal(err)
	}
	wait, err = restarted.ReserveAIRequest(ctx, time.Minute)
	if err != nil || wait < 59*time.Minute {
		t.Fatalf("success erased delay: %v %v", wait, err)
	}
}
