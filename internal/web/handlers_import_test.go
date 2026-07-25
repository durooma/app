package web

import (
	"bytes"
	"context"
	"fmt"
	"mime/multipart"
	"strings"
	"testing"

	"durooma/internal/importer"
)

type upload struct {
	name    string
	content string
}

// uploadHeaders builds real multipart file headers so that the tests exercise
// readImportFile the same way an HTTP upload does.
func uploadHeaders(t *testing.T, uploads ...upload) []*multipart.FileHeader {
	t.Helper()

	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	for _, u := range uploads {
		part, err := w.CreateFormFile("files", u.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write([]byte(u.content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	form, err := multipart.NewReader(&body, w.Boundary()).ReadForm(1 << 20)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { form.RemoveAll() })
	return form.File["files"]
}

func TestImportBatchAggregatesAcrossFiles(t *testing.T) {
	files := uploadHeaders(t,
		upload{"ubs.csv", "Trade date;Debit;Credit\n"},
		upload{"schwab.csv", "Date,Action,Amount\n"},
	)

	var seen []string
	batch := importBatch(context.Background(), files,
		func(_ context.Context, blob []byte) (importer.Result, error) {
			seen = append(seen, string(blob))
			return importer.Result{
				Parsed: 10, Inserted: 7, Duplicates: 3,
				Warnings: []string{"FX rate unavailable"},
			}, nil
		})

	if batch.Imported != 2 {
		t.Errorf("Imported = %d, want 2", batch.Imported)
	}
	if len(batch.Errors) != 0 {
		t.Errorf("unexpected errors: %v", batch.Errors)
	}
	if batch.Result.Parsed != 20 || batch.Result.Inserted != 14 || batch.Result.Duplicates != 6 {
		t.Errorf("totals not summed: %+v", batch.Result)
	}
	want := []string{
		"ubs.csv: FX rate unavailable",
		"schwab.csv: FX rate unavailable",
	}
	if strings.Join(batch.Result.Warnings, "|") != strings.Join(want, "|") {
		t.Errorf("warnings = %v, want %v", batch.Result.Warnings, want)
	}
	if len(seen) != 2 || seen[0] != "Trade date;Debit;Credit\n" {
		t.Errorf("importer saw %q", seen)
	}
}

// A file the importer rejects must not discard the files around it.
func TestImportBatchIsolatesFailures(t *testing.T) {
	files := uploadHeaders(t,
		upload{"good-1.csv", "a"},
		upload{"notes.csv", "b"},
		upload{"good-2.csv", "c"},
	)

	batch := importBatch(context.Background(), files,
		func(_ context.Context, blob []byte) (importer.Result, error) {
			if string(blob) == "b" {
				return importer.Result{}, fmt.Errorf("unsupported CSV structure")
			}
			return importer.Result{Parsed: 2, Inserted: 2}, nil
		})

	if batch.Imported != 2 {
		t.Errorf("Imported = %d, want 2", batch.Imported)
	}
	if batch.Result.Inserted != 4 {
		t.Errorf("Inserted = %d, want 4 (surviving files)", batch.Result.Inserted)
	}
	if len(batch.Errors) != 1 || !strings.HasPrefix(batch.Errors[0], "notes.csv: ") {
		t.Fatalf("errors = %v, want one naming notes.csv", batch.Errors)
	}
	if !strings.Contains(batch.Errors[0], "unsupported CSV structure") {
		t.Errorf("error lost the cause: %q", batch.Errors[0])
	}
}

func TestImportBatchRejectsOversizeFile(t *testing.T) {
	files := uploadHeaders(t,
		upload{"huge.csv", strings.Repeat("x", maxImportFileBytes+1)},
		upload{"small.csv", "ok"},
	)

	called := 0
	batch := importBatch(context.Background(), files,
		func(_ context.Context, blob []byte) (importer.Result, error) {
			called++
			if len(blob) > maxImportFileBytes {
				t.Errorf("oversize blob reached the importer: %d bytes", len(blob))
			}
			return importer.Result{Parsed: 1, Inserted: 1}, nil
		})

	if called != 1 || batch.Imported != 1 {
		t.Errorf("called = %d, Imported = %d, want 1 and 1", called, batch.Imported)
	}
	if len(batch.Errors) != 1 || !strings.Contains(batch.Errors[0], "huge.csv: file exceeds") {
		t.Fatalf("errors = %v, want one size rejection for huge.csv", batch.Errors)
	}
}
