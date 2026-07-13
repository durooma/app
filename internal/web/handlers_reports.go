package web

import (
	"net/http"
	"sort"
	"time"

	"durooma/internal/models"
)

// splitIncomeExpense separates category totals into income (net positive) and
// expense (net negative) buckets and returns their sums.
func splitIncomeExpense(totals []models.CategoryTotal) (income, expense []models.CategoryTotal, incomeSum, expenseSum float64) {
	for _, t := range totals {
		if t.Amount >= 0 {
			income = append(income, t)
			incomeSum += t.Amount
		} else {
			expense = append(expense, t)
			expenseSum += t.Amount
		}
	}
	return
}

func (s *Server) handleYearDeepDive(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	year := intParam(r, "year", time.Now().Year())

	totals, err := s.store.CategoryTotalsForYear(ctx, year)
	if err != nil {
		s.fail(w, err)
		return
	}
	months, err := s.store.MonthTotalsForYear(ctx, year)
	if err != nil {
		s.fail(w, err)
		return
	}
	income, expense, incomeSum, expenseSum := splitIncomeExpense(totals)

	data := s.base(ctx, "Yearly deep dive", "year")
	data["Year"] = year
	data["Income"] = income
	data["Expense"] = expense
	data["IncomeSum"] = incomeSum
	data["ExpenseSum"] = expenseSum
	data["Net"] = incomeSum + expenseSum
	data["Months"] = months
	s.templates.render(w, "year_deepdive", data)
}

func (s *Server) handleMonthDeepDive(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	year := intParam(r, "year", time.Now().Year())
	month := intParam(r, "month", int(time.Now().Month()))

	totals, err := s.store.CategoryTotalsForMonth(ctx, year, month)
	if err != nil {
		s.fail(w, err)
		return
	}
	income, expense, incomeSum, expenseSum := splitIncomeExpense(totals)

	data := s.base(ctx, "Monthly deep dive", "month")
	data["Year"] = year
	data["Month"] = month
	data["Income"] = income
	data["Expense"] = expense
	data["IncomeSum"] = incomeSum
	data["ExpenseSum"] = expenseSum
	data["Net"] = incomeSum + expenseSum
	s.templates.render(w, "month_deepdive", data)
}

// matrixRow is a category with its 12 monthly values (index 1..12) and a total.
type matrixRow struct {
	Category   string
	CategoryID *int64
	Months     [13]float64
	Total      float64
}

func (s *Server) handleYearOverview(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	year := intParam(r, "year", time.Now().Year())

	cells, err := s.store.YearMatrix(ctx, year)
	if err != nil {
		s.fail(w, err)
		return
	}
	months, err := s.store.MonthTotalsForYear(ctx, year)
	if err != nil {
		s.fail(w, err)
		return
	}

	rowByCat := map[string]*matrixRow{}
	for _, c := range cells {
		row, ok := rowByCat[c.CategoryName]
		if !ok {
			row = &matrixRow{Category: c.CategoryName, CategoryID: c.CategoryID}
			rowByCat[c.CategoryName] = row
		}
		if c.Month >= 1 && c.Month <= 12 {
			row.Months[c.Month] += c.Amount
			row.Total += c.Amount
		}
	}
	rows := make([]*matrixRow, 0, len(rowByCat))
	for _, r := range rowByCat {
		rows = append(rows, r)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Total > rows[j].Total })

	data := s.base(ctx, "Year overview", "overview")
	data["Year"] = year
	data["Rows"] = rows
	data["Months"] = months
	s.templates.render(w, "year_overview", data)
}

// yearMatrixRow is a category with per-year values for the multi-year view.
type yearMatrixRow struct {
	Category   string
	CategoryID *int64
	ByYear     map[int]float64
	Total      float64
}

func (s *Server) handleMultiYear(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	yearTotals, err := s.store.YearTotals(ctx)
	if err != nil {
		s.fail(w, err)
		return
	}
	matrix, err := s.store.CategoryMatrixForYears(ctx)
	if err != nil {
		s.fail(w, err)
		return
	}

	years := make([]int, 0, len(yearTotals))
	for _, yt := range yearTotals {
		years = append(years, yt.Year)
	}

	rowByCat := map[string]*yearMatrixRow{}
	for _, m := range matrix {
		row, ok := rowByCat[m.CategoryName]
		if !ok {
			row = &yearMatrixRow{Category: m.CategoryName, CategoryID: m.CategoryID, ByYear: map[int]float64{}}
			rowByCat[m.CategoryName] = row
		}
		row.ByYear[m.Year] += m.Amount
		row.Total += m.Amount
	}
	rows := make([]*yearMatrixRow, 0, len(rowByCat))
	for _, r := range rowByCat {
		rows = append(rows, r)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Total > rows[j].Total })

	data := s.base(ctx, "Multi-year overview", "years")
	data["Years"] = years
	data["YearTotals"] = yearTotals
	data["Rows"] = rows
	s.templates.render(w, "multi_year", data)
}
