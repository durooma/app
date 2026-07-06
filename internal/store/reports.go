package store

import (
	"context"

	"durooma/internal/models"
)

// allocCTE expands every transaction into one row per month in its amortization
// window, dividing base_amount evenly across those months. This is what makes a
// transaction assigned to a quarter or a whole year contribute a fair share to
// each month in all reports below.
const allocCTE = `
WITH alloc AS (
	SELECT
		t.category_id,
		COALESCE(c.name, '(uncategorized)') AS category_name,
		COALESCE(c.kind, '')                AS kind,
		gs.m::date                          AS month,
		t.base_amount / GREATEST(1,
			(EXTRACT(YEAR FROM t.end_month)::int * 12 + EXTRACT(MONTH FROM t.end_month)::int)
		  - (EXTRACT(YEAR FROM t.start_month)::int * 12 + EXTRACT(MONTH FROM t.start_month)::int) + 1
		) AS amount
	FROM transactions t
	LEFT JOIN categories c ON c.id = t.category_id
	CROSS JOIN LATERAL generate_series(t.start_month, t.end_month, interval '1 month') AS gs(m)
)`

// AvailableYears lists the distinct years that have allocated activity.
func (s *Store) AvailableYears(ctx context.Context) ([]int, error) {
	rows, err := s.pool.Query(ctx, allocCTE+`
		SELECT DISTINCT EXTRACT(YEAR FROM month)::int AS y FROM alloc ORDER BY y DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int
	for rows.Next() {
		var y int
		if err := rows.Scan(&y); err != nil {
			return nil, err
		}
		out = append(out, y)
	}
	return out, rows.Err()
}

// CategoryTotalsForYear returns the net amount per category for a year.
func (s *Store) CategoryTotalsForYear(ctx context.Context, year int) ([]models.CategoryTotal, error) {
	return s.categoryTotals(ctx, allocCTE+`
		SELECT category_id, category_name, kind, SUM(amount)
		FROM alloc WHERE EXTRACT(YEAR FROM month)::int = $1
		GROUP BY category_id, category_name, kind
		ORDER BY SUM(amount) DESC`, year)
}

// CategoryTotalsForMonth returns the net amount per category for one month.
func (s *Store) CategoryTotalsForMonth(ctx context.Context, year, month int) ([]models.CategoryTotal, error) {
	return s.categoryTotals(ctx, allocCTE+`
		SELECT category_id, category_name, kind, SUM(amount)
		FROM alloc
		WHERE EXTRACT(YEAR FROM month)::int = $1 AND EXTRACT(MONTH FROM month)::int = $2
		GROUP BY category_id, category_name, kind
		ORDER BY SUM(amount) DESC`, year, month)
}

func (s *Store) categoryTotals(ctx context.Context, q string, args ...any) ([]models.CategoryTotal, error) {
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.CategoryTotal
	for rows.Next() {
		var c models.CategoryTotal
		if err := rows.Scan(&c.CategoryID, &c.CategoryName, &c.Kind, &c.Amount); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// MonthTotalsForYear returns income/expense/net for each of the 12 months.
func (s *Store) MonthTotalsForYear(ctx context.Context, year int) ([]models.MonthTotal, error) {
	rows, err := s.pool.Query(ctx, allocCTE+`
		SELECT EXTRACT(MONTH FROM month)::int AS m,
		       COALESCE(SUM(amount) FILTER (WHERE amount > 0), 0),
		       COALESCE(SUM(amount) FILTER (WHERE amount < 0), 0)
		FROM alloc WHERE EXTRACT(YEAR FROM month)::int = $1
		GROUP BY m ORDER BY m`, year)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	byMonth := map[int]models.MonthTotal{}
	for rows.Next() {
		var mt models.MonthTotal
		if err := rows.Scan(&mt.Month, &mt.Income, &mt.Expense); err != nil {
			return nil, err
		}
		mt.Net = mt.Income + mt.Expense
		byMonth[mt.Month] = mt
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Emit all 12 months so the overview grid is always complete.
	out := make([]models.MonthTotal, 0, 12)
	for m := 1; m <= 12; m++ {
		if mt, ok := byMonth[m]; ok {
			out = append(out, mt)
		} else {
			out = append(out, models.MonthTotal{Month: m})
		}
	}
	return out, nil
}

// YearTotals returns income/expense/net per year for the multi-year overview.
func (s *Store) YearTotals(ctx context.Context) ([]models.YearTotal, error) {
	rows, err := s.pool.Query(ctx, allocCTE+`
		SELECT EXTRACT(YEAR FROM month)::int AS y,
		       COALESCE(SUM(amount) FILTER (WHERE amount > 0), 0),
		       COALESCE(SUM(amount) FILTER (WHERE amount < 0), 0)
		FROM alloc GROUP BY y ORDER BY y`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.YearTotal
	for rows.Next() {
		var yt models.YearTotal
		if err := rows.Scan(&yt.Year, &yt.Income, &yt.Expense); err != nil {
			return nil, err
		}
		yt.Net = yt.Income + yt.Expense
		out = append(out, yt)
	}
	return out, rows.Err()
}

// YearMatrix returns one cell per (month, category) for the year — the data for
// the "all months × categories" grid.
func (s *Store) YearMatrix(ctx context.Context, year int) ([]models.MonthCategoryCell, error) {
	rows, err := s.pool.Query(ctx, allocCTE+`
		SELECT EXTRACT(MONTH FROM month)::int AS m, category_id, category_name, SUM(amount)
		FROM alloc WHERE EXTRACT(YEAR FROM month)::int = $1
		GROUP BY m, category_id, category_name
		ORDER BY category_name, m`, year)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.MonthCategoryCell
	for rows.Next() {
		var cell models.MonthCategoryCell
		if err := rows.Scan(&cell.Month, &cell.CategoryID, &cell.CategoryName, &cell.Amount); err != nil {
			return nil, err
		}
		out = append(out, cell)
	}
	return out, rows.Err()
}

// CategoryMatrixForYears returns net per (year, category) for a multi-year,
// cross-category comparison.
func (s *Store) CategoryMatrixForYears(ctx context.Context) ([]struct {
	Year         int
	CategoryName string
	Amount       float64
}, error) {
	rows, err := s.pool.Query(ctx, allocCTE+`
		SELECT EXTRACT(YEAR FROM month)::int AS y, category_name, SUM(amount)
		FROM alloc GROUP BY y, category_name ORDER BY category_name, y`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []struct {
		Year         int
		CategoryName string
		Amount       float64
	}
	for rows.Next() {
		var r struct {
			Year         int
			CategoryName string
			Amount       float64
		}
		if err := rows.Scan(&r.Year, &r.CategoryName, &r.Amount); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
