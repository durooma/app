package web

import (
	"net/http"
	"net/url"
	"sort"
	"strconv"

	"durooma/internal/models"
	"durooma/internal/store"
)

// txnLimit caps the transaction list shown when drilling all the way into a
// single month; the full list stays one click away in the transactions view.
const txnLimit = 500

// breakdownCell is one category's amount in one sub-period of the report scope.
type breakdownCell struct {
	Period models.Period
	Amount float64
}

// breakdownRow is one category within the report scope: its total plus the
// value in each sub-period column (aligned with the Subs slice).
type breakdownRow struct {
	Category   string
	CategoryID *int64
	Total      float64
	Cells      []breakdownCell
}

// buildBreakdown turns the scope's category totals and its per-sub-period cells
// into the income and expense tables. Each side counts only the allocations of
// that sign, so the tables add up to the period's totals and a category holding
// both — an expense with a refund, a mixed uncategorized bucket — appears in
// both rather than being netted into one.
func buildBreakdown(totals []models.CategoryTotal, subs []models.PeriodTotal,
	cells []models.PeriodCategoryCell) (income, expense []breakdownRow) {

	byCategory := map[string]map[models.Period]models.PeriodCategoryCell{}
	for _, c := range cells {
		if byCategory[c.CategoryName] == nil {
			byCategory[c.CategoryName] = map[models.Period]models.PeriodCategoryCell{}
		}
		cell := byCategory[c.CategoryName][c.Period]
		cell.Income += c.Income
		cell.Expense += c.Expense
		byCategory[c.CategoryName][c.Period] = cell
	}
	// side builds one row of one table, picking the same side out of the category
	// total and out of every sub-period cell.
	side := func(t models.CategoryTotal, total float64, pick func(models.PeriodCategoryCell) float64) breakdownRow {
		row := breakdownRow{Category: t.CategoryName, CategoryID: t.CategoryID, Total: total}
		for _, s := range subs {
			row.Cells = append(row.Cells, breakdownCell{
				Period: s.Period,
				Amount: pick(byCategory[t.CategoryName][s.Period]),
			})
		}
		return row
	}
	for _, t := range totals {
		if t.Income != 0 {
			income = append(income, side(t, t.Income,
				func(c models.PeriodCategoryCell) float64 { return c.Income }))
		}
		if t.Expense != 0 {
			expense = append(expense, side(t, t.Expense,
				func(c models.PeriodCategoryCell) float64 { return c.Expense }))
		}
	}
	// Biggest first on both sides.
	sort.SliceStable(income, func(i, j int) bool { return income[i].Total > income[j].Total })
	sort.SliceStable(expense, func(i, j int) bool { return expense[i].Total < expense[j].Total })
	return income, expense
}

// handleReports renders the single report view at whichever scope the request
// asks for: all time, one year, or one month. Each level shows the same three
// things — the period's totals, its category breakdown, and the list of
// sub-periods to drill into — plus the transactions themselves once there is
// nothing left to drill into.
func (s *Server) handleReports(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	p := periodFromRequest(r)

	totals, err := s.store.Totals(ctx, p)
	if err != nil {
		s.fail(w, err)
		return
	}
	catTotals, err := s.store.CategoryTotals(ctx, p)
	if err != nil {
		s.fail(w, err)
		return
	}
	subs, err := s.store.SubPeriodTotals(ctx, p)
	if err != nil {
		s.fail(w, err)
		return
	}
	cells, err := s.store.CategoryMatrix(ctx, p)
	if err != nil {
		s.fail(w, err)
		return
	}
	income, expense := buildBreakdown(catTotals, subs, cells)

	data := s.base(ctx, "Reports — "+p.Label(), "reports")
	data["Period"] = p
	data["Crumbs"] = crumbs(p)
	data["Totals"] = totals
	data["Income"] = income
	data["Expense"] = expense
	data["Subs"] = subs

	// At month level there is no further period to drill into, so the individual
	// transactions take the place of the sub-period list.
	if p.IsMonth() {
		start, end := p.Bounds()
		txns, err := s.store.ListTransactions(ctx, store.TxnFilter{
			PeriodStart: start, PeriodEnd: end, Limit: txnLimit,
		})
		if err != nil {
			s.fail(w, err)
			return
		}
		alloc := make(map[int64]float64, len(txns))
		for _, t := range txns {
			alloc[t.ID] = t.AllocatedFor(start, end)
		}
		data["Transactions"] = txns
		data["Alloc"] = alloc
		data["TxnsTruncated"] = len(txns) == txnLimit
	}
	s.templates.render(w, "reports", data)
}

// periodFromRequest reads the report scope from ?year=&month=, ignoring a month
// given without a year and any month outside 1-12.
func periodFromRequest(r *http.Request) models.Period {
	p := models.Period{Year: intParam(r, "year", 0), Month: intParam(r, "month", 0)}
	if p.Month < 1 || p.Month > 12 {
		p.Month = 0
	}
	if p.Year == 0 {
		p.Month = 0
	}
	return p
}

// crumb is one step of the scope breadcrumb.
type crumb struct {
	Label   string
	URL     string
	Current bool
}

// crumbs builds the trail from all time down to the current scope.
func crumbs(p models.Period) []crumb {
	trail := []models.Period{{}}
	if !p.IsAll() {
		trail = append(trail, models.Period{Year: p.Year})
	}
	if p.IsMonth() {
		trail = append(trail, p)
	}
	out := make([]crumb, 0, len(trail))
	for _, t := range trail {
		out = append(out, crumb{Label: t.Label(), URL: reportURL(t), Current: t == p})
	}
	return out
}

// reportURL is the report view at a given scope.
func reportURL(p models.Period) string {
	q := url.Values{}
	if p.Year != 0 {
		q.Set("year", strconv.Itoa(p.Year))
	}
	if p.Month != 0 {
		q.Set("month", strconv.Itoa(p.Month))
	}
	if len(q) == 0 {
		return "/reports"
	}
	return "/reports?" + q.Encode()
}

// handleLegacyReport redirects the pre-consolidation report URLs
// (/reports/year, /reports/month, /reports/year-overview, /reports/years) to the
// single report view, keeping whichever year and month they carried.
func (s *Server) handleLegacyReport(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, reportURL(periodFromRequest(r)), http.StatusFound)
}
