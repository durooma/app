package web

import (
	"net/http"
	"time"

	"durooma/internal/models"
	"durooma/internal/store"
)

const pageSize = 200

// txnFilterFromRequest builds a paged transaction filter from the request's
// query/form values.
func txnFilterFromRequest(r *http.Request) store.TxnFilter {
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
	f.PeriodStart, f.PeriodEnd = parsePeriod(r.FormValue("period"))
	switch r.FormValue("sign") {
	case "income", "expense":
		f.Sign = r.FormValue("sign")
	}
	return f
}

// parsePeriod interprets a drill-down period param as either a single month
// ("2006-01") or a whole year ("2006"), returning the inclusive first-of-month
// bounds. A blank/invalid value yields zero times (no period filter).
func parsePeriod(v string) (start, end time.Time) {
	if v == "" {
		return
	}
	if t, err := time.Parse("2006-01", v); err == nil {
		s := time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
		return s, s
	}
	if t, err := time.Parse("2006", v); err == nil {
		return time.Date(t.Year(), 1, 1, 0, 0, 0, 0, time.UTC),
			time.Date(t.Year(), 12, 1, 0, 0, 0, 0, time.UTC)
	}
	return
}

// transactionsData runs the current filter and assembles the template data for
// the transactions page. Callers may add extra keys (e.g. a categorization
// Report) before rendering.
func (s *Server) transactionsData(r *http.Request, f store.TxnFilter) (map[string]any, error) {
	ctx := r.Context()
	txns, err := s.store.ListTransactions(ctx, f)
	if err != nil {
		return nil, err
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
		"period":         r.FormValue("period"),
		"sign":           r.FormValue("sign"),
	}
	data["AIEnabled"] = s.cfg.AIEnabled()

	// When drilling into a period, expose each transaction's amortized share of
	// that period so amortized rows can show it alongside the full amount. The
	// sum of shares reconciles with the report figure that was clicked.
	if !f.PeriodStart.IsZero() && !f.PeriodEnd.IsZero() {
		alloc := make(map[int64]float64, len(txns))
		var allocTotal float64
		for _, t := range txns {
			share := t.AllocatedFor(f.PeriodStart, f.PeriodEnd)
			alloc[t.ID] = share
			allocTotal += share
		}
		data["PeriodActive"] = true
		data["Alloc"] = alloc
		data["AllocTotal"] = allocTotal
	}
	return data, nil
}

func (s *Server) handleTransactions(w http.ResponseWriter, r *http.Request) {
	data, err := s.transactionsData(r, txnFilterFromRequest(r))
	if err != nil {
		s.fail(w, err)
		return
	}
	s.templates.render(w, "transactions", data)
}

// handleCategorizeAll auto-categorizes every uncategorized transaction matching
// the current filter, then re-renders the page with a summary report. Existing
// categorizations are left untouched by the AI service.
func (s *Server) handleCategorizeAll(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Categorize across the whole matching set (not just the current page) but
	// only the still-uncategorized ones.
	catFilter := txnFilterFromRequest(r)
	catFilter.Uncategorized = true
	catFilter.Limit = 0
	catFilter.Offset = 0

	txns, err := s.store.ListTransactions(ctx, catFilter)
	if err != nil {
		s.fail(w, err)
		return
	}
	report, err := s.ai.Categorize(ctx, txns)
	if err != nil {
		s.fail(w, err)
		return
	}

	data, err := s.transactionsData(r, txnFilterFromRequest(r))
	if err != nil {
		s.fail(w, err)
		return
	}
	data["Report"] = report
	s.templates.render(w, "transactions", data)
}

// handleCategorizeOne auto-categorizes a single transaction (no-op if it is
// already categorized) and returns the re-rendered row for an HTMX swap.
func (s *Server) handleCategorizeOne(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := int64PathValue(r, "id")
	if err != nil {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	txn, err := s.store.GetTransaction(ctx, id)
	if err != nil {
		s.fail(w, err)
		return
	}
	if _, err := s.ai.Categorize(ctx, []models.Transaction{txn}); err != nil {
		s.fail(w, err)
		return
	}
	// Reload so the returned row reflects the freshly written category.
	txn, err = s.store.GetTransaction(ctx, id)
	if err != nil {
		s.fail(w, err)
		return
	}
	categories, _ := s.store.ListCategories(ctx)
	s.templates.renderPartial(w, "transactions", "txn-row", map[string]any{
		"T":         txn,
		"Cats":      categories,
		"AIEnabled": s.cfg.AIEnabled(),
	})
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
