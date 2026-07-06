package web

import (
	"io"
	"net/http"
)

func (s *Server) handleImportForm(w http.ResponseWriter, r *http.Request) {
	data := s.base(r.Context(), "Import", "import")
	s.templates.render(w, "import", data)
}

func (s *Server) handleImport(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if err := r.ParseMultipartForm(16 << 20); err != nil { // 16 MB cap
		http.Error(w, "could not parse upload: "+err.Error(), http.StatusBadRequest)
		return
	}
	provider := r.FormValue("provider")
	account := r.FormValue("account")

	file, _, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "no file uploaded", http.StatusBadRequest)
		return
	}
	defer file.Close()
	blob, err := io.ReadAll(io.LimitReader(file, 16<<20))
	if err != nil {
		s.fail(w, err)
		return
	}

	result, err := s.importer.Import(ctx, provider, account, blob)

	data := s.base(ctx, "Import", "import")
	if err != nil {
		data["Error"] = err.Error()
	} else {
		data["Result"] = result
	}
	s.templates.render(w, "import", data)
}

func (s *Server) handleCategorize(w http.ResponseWriter, r *http.Request) {
	report, err := s.ai.Categorize(r.Context())

	data := s.base(r.Context(), "Import", "import")
	if err != nil {
		data["Error"] = err.Error()
	} else {
		data["Report"] = report
	}
	s.templates.render(w, "import", data)
}
