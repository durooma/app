package web

import (
	"net/http"
	"time"

	"durooma/internal/store"
)

const pageSize = 200

func (s *Server) handleTransactions(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	f := store.TxnFilter{
		AccountID:     int64(intParam(r, "account_id", 0)),
		InstitutionID: int64(intParam(r, "institution_id", 0)),
		CategoryID:    int64(intParam(r, "category_id", 0)),
		Uncategorized: r.FormValue("uncategorized") == "1",
		Search:        r.FormValue("q"),
		Limit:         pageSize,
		Offset:        (intParam(r, "page", 1) - 1) * pageSize,
	}
	if v := r.FormValue("from"); v != "" {
		if t, err := time.Parse("2006-01-02", v); err == nil {
			f.From = t
		}
	}
	if v := r.FormValue("to"); v != "" {
		if t, err := time.Parse("2006-01-02", v); err == nil {
			f.To = t
		}
	}

	txns, err := s.store.ListTransactions(ctx, f)
	if err != nil {
		s.fail(w, err)
		return
	}
	accounts, _ := s.store.ListAccounts(ctx)
	institutions, _ := s.store.ListInstitutions(ctx)
	categories, _ := s.store.ListCategories(ctx)

	var total float64
	for _, t := range txns {
		total += t.BaseAmount
	}

	data := s.base(ctx, "Transactions", "transactions")
	data["Transactions"] = txns
	data["Accounts"] = accounts
	data["Institutions"] = institutions
	data["Categories"] = categories
	data["Total"] = total
	data["Page"] = intParam(r, "page", 1)
	data["HasMore"] = len(txns) == pageSize
	data["Filter"] = map[string]any{
		"account_id":     f.AccountID,
		"institution_id": f.InstitutionID,
		"category_id":    f.CategoryID,
		"uncategorized":  f.Uncategorized,
		"q":              f.Search,
		"from":           r.FormValue("from"),
		"to":             r.FormValue("to"),
	}
	s.templates.render(w, "transactions", data)
}

func (s *Server) handleSetCategory(w http.ResponseWriter, r *http.Request) {
	id, err := int64PathValue(r, "id")
	if err != nil {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	var catID *int64
	if v := intParam(r, "category_id", 0); v != 0 {
		c := int64(v)
		catID = &c
	}
	if err := s.store.UpdateTransactionCategory(r.Context(), id, catID); err != nil {
		s.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleSetMonths(w http.ResponseWriter, r *http.Request) {
	id, err := int64PathValue(r, "id")
	if err != nil {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	start, err1 := time.Parse("2006-01", r.FormValue("start_month"))
	end, err2 := time.Parse("2006-01", r.FormValue("end_month"))
	if err1 != nil || err2 != nil {
		http.Error(w, "invalid month (expected YYYY-MM)", http.StatusBadRequest)
		return
	}
	if end.Before(start) {
		start, end = end, start
	}
	if err := s.store.UpdateTransactionMonths(r.Context(), id, start, end); err != nil {
		s.fail(w, err)
		return
	}
	// Full reload so every report reflects the new amortization window.
	http.Redirect(w, r, r.Header.Get("Referer"), http.StatusSeeOther)
}

func (s *Server) handleSetNote(w http.ResponseWriter, r *http.Request) {
	id, err := int64PathValue(r, "id")
	if err != nil {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	if err := s.store.UpdateTransactionNote(r.Context(), id, r.FormValue("note")); err != nil {
		s.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleDeleteTransaction(w http.ResponseWriter, r *http.Request) {
	id, err := int64PathValue(r, "id")
	if err != nil {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	if err := s.store.DeleteTransaction(r.Context(), id); err != nil {
		s.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusOK) // HTMX removes the row
}
