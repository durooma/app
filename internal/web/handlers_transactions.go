package web

import (
	"context"
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

// maxCategorizeRun bounds how many transactions one run pulls into memory. It
// is applied as a query limit rather than by discarding rows already loaded, so
// the bound actually saves the memory it is there to save on a small host.
// Anything beyond it is reported as still uncategorized, not silently dropped.
const maxCategorizeRun = 5000

// handleCategorizeAll starts a background run over the uncategorized
// transactions matching the current filter and returns the progress bar. The
// run is a provider round trip per batch, so it outlives this request rather
// than holding it open; the bar polls handleCategorizeStatus for the outcome.
// Existing categorizations are left untouched by the AI service.
func (s *Server) handleCategorizeAll(w http.ResponseWriter, r *http.Request) {
	// Categorize across the whole matching set (not just the current page) but
	// only the still-uncategorized ones.
	catFilter := txnFilterFromRequest(r)
	catFilter.Uncategorized = true
	catFilter.Limit = maxCategorizeRun
	catFilter.Offset = 0

	txns, err := s.store.ListTransactions(r.Context(), catFilter)
	if err != nil {
		s.fail(w, err)
		return
	}
	run := catRun{
		svc:  s.ai,
		txns: txns,
		remaining: func(ctx context.Context) (int, error) {
			return s.store.CountTransactions(ctx, catFilter)
		},
	}
	if !s.catJob.start(run) {
		// A run is already in flight and its bar is already on screen; 204
		// tells htmx to leave the page alone rather than adding a second one.
		w.WriteHeader(http.StatusNoContent)
		return
	}
	s.renderCatProgress(w)
}

// handleCategorizeStatus serves the progress bar that the bar itself polls.
func (s *Server) handleCategorizeStatus(w http.ResponseWriter, r *http.Request) {
	s.renderCatProgress(w)
}

// handleCategorizeAbort stops the running job. Work already committed stays:
// aborting halts the run, it does not roll back the categories written so far.
func (s *Server) handleCategorizeAbort(w http.ResponseWriter, r *http.Request) {
	s.catJob.abort()
	s.renderCatProgress(w)
}

// handleCategorizeDismiss clears a finished run's summary bar.
func (s *Server) handleCategorizeDismiss(w http.ResponseWriter, r *http.Request) {
	s.catJob.dismiss()
	s.renderCatProgress(w)
}

// renderCatProgress writes the progress bar fragment. With no job to report it
// writes nothing, which an outerHTML swap turns into removing the bar.
func (s *Server) renderCatProgress(w http.ResponseWriter) {
	s.templates.renderPartial(w, "transactions", "cat-progress", s.catJob.status())
}

// handleCategorizeOne uses the background job too: even one transaction may
// need to wait for the shared quota, so it must not hold an HTTP request open.
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
	s.catJob.start(catRun{svc: s.ai, txns: []models.Transaction{txn}})
	s.renderCatProgress(w)
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
