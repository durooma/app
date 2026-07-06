package importer

import (
	"context"
	"os"
	"testing"
	"time"

	"durooma/internal/db"
	"durooma/internal/store"
)

// stubFX errors on use; UBS imports are CHF so no conversion should occur.
type stubFX struct{ called bool }

func (s *stubFX) Convert(context.Context, time.Time, float64, string, string) (float64, error) {
	s.called = true
	return 0, context.Canceled
}

const ubsCSV = `UBS Account Statement;;;;;;;;;;;
Trade date;Trade time;Booking date;Value date;Ccy;Debit;Credit;Sub;Bal;X;Description1;Description2
15.01.2024;10:00;15.01.2024;15.01.2024;CHF;-120.50;;;;;Migros;Zurich
25.01.2024;09:00;25.01.2024;25.01.2024;CHF;;5000.00;;;;Salary;ACME Corp
03.02.2024;12:00;03.02.2024;03.02.2024;CHF;-2400.00;;;;;Insurance Annual;Helvetia
`

func TestImportUBSIntegration(t *testing.T) {
	url := os.Getenv("DUROOMA_TEST_DB")
	if url == "" {
		t.Skip("set DUROOMA_TEST_DB to run importer integration test")
	}
	ctx := context.Background()
	pool, err := db.Connect(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	_, _ = pool.Exec(ctx, `TRUNCATE transactions, accounts, institutions RESTART IDENTITY CASCADE`)

	st := store.New(pool)
	fx := &stubFX{}
	im := New(st, fx, "CHF")

	res, err := im.Import(ctx, "UBS", "UBS Main", []byte(ubsCSV))
	if err != nil {
		t.Fatal(err)
	}
	if res.Inserted != 3 || res.Parsed != 3 {
		t.Fatalf("first import: parsed=%d inserted=%d, want 3/3", res.Parsed, res.Inserted)
	}
	if fx.called {
		t.Error("FX converter should not be called for CHF transactions")
	}

	// Re-import: everything is a duplicate.
	res2, err := im.Import(ctx, "UBS", "UBS Main", []byte(ubsCSV))
	if err != nil {
		t.Fatal(err)
	}
	if res2.Inserted != 0 || res2.Duplicates != 3 {
		t.Fatalf("re-import: inserted=%d duplicates=%d, want 0/3", res2.Inserted, res2.Duplicates)
	}
}
