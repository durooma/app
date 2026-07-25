package web

import (
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"

	"durooma/internal/importer"
)

const (
	maxImportFileBytes  = 16 << 20 // per CSV
	maxImportBatchBytes = 64 << 20 // whole upload
	// multipartMemoryBytes is how much of the upload is buffered in RAM; the
	// remainder spills to temp files that net/http removes after the request.
	multipartMemoryBytes = 16 << 20
	maxImportFiles       = 50
)

func (s *Server) handleImportForm(w http.ResponseWriter, r *http.Request) {
	data := s.base(r.Context(), "Import", "import")
	s.templates.render(w, "import", data)
}

func (s *Server) handleImport(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	r.Body = http.MaxBytesReader(w, r.Body, maxImportBatchBytes)
	if err := r.ParseMultipartForm(multipartMemoryBytes); err != nil {
		http.Error(w, "could not parse upload: "+err.Error(), http.StatusBadRequest)
		return
	}
	account := r.FormValue("account")
	provider := r.FormValue("provider") // optional API override; blank = detect per file

	files := append([]*multipart.FileHeader(nil), r.MultipartForm.File["files"]...)
	files = append(files, r.MultipartForm.File["file"]...) // backwards-compatible API field
	if len(files) == 0 {
		http.Error(w, "no file uploaded", http.StatusBadRequest)
		return
	}
	if len(files) > maxImportFiles {
		http.Error(w, fmt.Sprintf("too many files: %d (limit is %d per upload)",
			len(files), maxImportFiles), http.StatusBadRequest)
		return
	}

	batch := importBatch(ctx, files, func(ctx context.Context, blob []byte) (importer.Result, error) {
		if provider != "" {
			return s.importer.Import(ctx, provider, account, blob)
		}
		return s.importer.ImportAuto(ctx, account, blob)
	})

	data := s.base(ctx, "Import", "import")
	if batch.Imported > 0 {
		data["Result"] = batch.Result
		data["FilesImported"] = batch.Imported
		data["FilesTotal"] = len(files)
	}
	if len(batch.Errors) > 0 {
		data["Error"] = strings.Join(batch.Errors, "; ")
	}
	s.templates.render(w, "import", data)
}

// batchResult aggregates one multi-file import run.
type batchResult struct {
	Result   importer.Result
	Imported int
	Errors   []string
}

// importBatch imports each uploaded file independently so that one unreadable
// or unrecognized file does not discard the rest, tagging that file's warnings
// and errors with its name.
func importBatch(ctx context.Context, files []*multipart.FileHeader,
	importFn func(context.Context, []byte) (importer.Result, error)) batchResult {

	var batch batchResult
	for _, header := range files {
		blob, err := readImportFile(header)
		if err != nil {
			batch.Errors = append(batch.Errors, fmt.Sprintf("%s: %v", header.Filename, err))
			continue
		}

		result, err := importFn(ctx, blob)
		if err != nil {
			batch.Errors = append(batch.Errors, fmt.Sprintf("%s: %v", header.Filename, err))
			continue
		}

		batch.Imported++
		batch.Result.Parsed += result.Parsed
		batch.Result.Inserted += result.Inserted
		batch.Result.Duplicates += result.Duplicates
		for _, warning := range result.Warnings {
			batch.Result.Warnings = append(batch.Result.Warnings,
				fmt.Sprintf("%s: %s", header.Filename, warning))
		}
	}
	return batch
}

func readImportFile(header *multipart.FileHeader) ([]byte, error) {
	file, err := header.Open()
	if err != nil {
		return nil, err
	}
	defer file.Close()

	blob, err := io.ReadAll(io.LimitReader(file, maxImportFileBytes+1))
	if err != nil {
		return nil, err
	}
	if len(blob) > maxImportFileBytes {
		return nil, fmt.Errorf("file exceeds the %d MB limit", maxImportFileBytes>>20)
	}
	return blob, nil
}
