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

// scopeWhere restricts alloc rows to the given period. All time needs no
// predicate; the args are positional so they must be passed first to any query
// that uses it.
func scopeWhere(p models.Period) (string, []any) {
	switch {
	case p.IsMonth():
		return "WHERE EXTRACT(YEAR FROM month)::int = $1 AND EXTRACT(MONTH FROM month)::int = $2",
			[]any{p.Year, p.Month}
	case p.IsYear():
		return "WHERE EXTRACT(YEAR FROM month)::int = $1", []any{p.Year}
	}
	return "", nil
}

// subBucket returns the SQL expression grouping a period's rows into the
// sub-periods one level down (years inside all time, months inside a year), and
// whether such a level exists at all.
func subBucket(p models.Period) (string, bool) {
	switch {
	case p.IsMonth():
		return "", false
	case p.IsYear():
		return "EXTRACT(MONTH FROM month)::int", true
	}
	return "EXTRACT(YEAR FROM month)::int", true
}

// subPeriod rebuilds a sub-period from its bucket value.
func subPeriod(p models.Period, bucket int) models.Period {
	if p.IsYear() {
		return models.Period{Year: p.Year, Month: bucket}
	}
	return models.Period{Year: bucket}
}

// Totals returns income/expense/net for a whole period. The split is made per
// allocated month-row, so these figures are exactly the sum of the period's
// sub-period totals.
func (s *Store) Totals(ctx context.Context, p models.Period) (models.PeriodTotal, error) {
	where, args := scopeWhere(p)
	out := models.PeriodTotal{Period: p}
	err := s.pool.QueryRow(ctx, allocCTE+`
		SELECT COALESCE(SUM(amount) FILTER (WHERE amount > 0), 0),
		       COALESCE(SUM(amount) FILTER (WHERE amount < 0), 0)
		FROM alloc `+where, args...).Scan(&out.Income, &out.Expense)
	if err != nil {
		return out, err
	}
	out.Net = out.Income + out.Expense
	return out, nil
}

// CategoryTotals returns the income and expense totals per category within a
// period, uncategorized transactions included as their own bucket. Splitting by
// the sign of each allocated month — rather than by a category's net — is what
// makes the two sides add up to Totals.
func (s *Store) CategoryTotals(ctx context.Context, p models.Period) ([]models.CategoryTotal, error) {
	where, args := scopeWhere(p)
	rows, err := s.pool.Query(ctx, allocCTE+`
		SELECT category_id, category_name, kind,
		       COALESCE(SUM(amount) FILTER (WHERE amount > 0), 0),
		       COALESCE(SUM(amount) FILTER (WHERE amount < 0), 0)
		FROM alloc `+where+`
		GROUP BY category_id, category_name, kind
		ORDER BY SUM(amount) DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.CategoryTotal
	for rows.Next() {
		var c models.CategoryTotal
		if err := rows.Scan(&c.CategoryID, &c.CategoryName, &c.Kind, &c.Income, &c.Expense); err != nil {
			return nil, err
		}
		c.Net = c.Income + c.Expense
		out = append(out, c)
	}
	return out, rows.Err()
}

// SubPeriodTotals returns income/expense/net for each sub-period of p: one row
// per year with activity when p is all time, one row per month (all twelve,
// empty ones included) when p is a year, and nothing when p is a single month.
func (s *Store) SubPeriodTotals(ctx context.Context, p models.Period) ([]models.PeriodTotal, error) {
	bucket, ok := subBucket(p)
	if !ok {
		return nil, nil
	}
	where, args := scopeWhere(p)
	rows, err := s.pool.Query(ctx, allocCTE+`
		SELECT `+bucket+` AS b,
		       COALESCE(SUM(amount) FILTER (WHERE amount > 0), 0),
		       COALESCE(SUM(amount) FILTER (WHERE amount < 0), 0)
		FROM alloc `+where+`
		GROUP BY b ORDER BY b`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []models.PeriodTotal
	byBucket := map[int]models.PeriodTotal{}
	for rows.Next() {
		var b int
		var pt models.PeriodTotal
		if err := rows.Scan(&b, &pt.Income, &pt.Expense); err != nil {
			return nil, err
		}
		pt.Period = subPeriod(p, b)
		pt.Net = pt.Income + pt.Expense
		byBucket[b] = pt
		out = append(out, pt)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if !p.IsYear() {
		return out, nil
	}
	// Inside a year, emit all twelve months so the list is always complete.
	full := make([]models.PeriodTotal, 0, 12)
	for m := 1; m <= 12; m++ {
		pt, found := byBucket[m]
		if !found {
			pt = models.PeriodTotal{Period: models.Period{Year: p.Year, Month: m}}
		}
		full = append(full, pt)
	}
	return full, nil
}

// CategoryMatrix returns income and expenses per (sub-period, category) inside p
// — the cells of the breakdown grid, split by sign exactly like CategoryTotals.
// It is empty when p has no sub-periods.
func (s *Store) CategoryMatrix(ctx context.Context, p models.Period) ([]models.PeriodCategoryCell, error) {
	bucket, ok := subBucket(p)
	if !ok {
		return nil, nil
	}
	where, args := scopeWhere(p)
	rows, err := s.pool.Query(ctx, allocCTE+`
		SELECT `+bucket+` AS b, category_id, category_name,
		       COALESCE(SUM(amount) FILTER (WHERE amount > 0), 0),
		       COALESCE(SUM(amount) FILTER (WHERE amount < 0), 0)
		FROM alloc `+where+`
		GROUP BY b, category_id, category_name
		ORDER BY category_name, b`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.PeriodCategoryCell
	for rows.Next() {
		var b int
		var cell models.PeriodCategoryCell
		if err := rows.Scan(&b, &cell.CategoryID, &cell.CategoryName, &cell.Income, &cell.Expense); err != nil {
			return nil, err
		}
		cell.Period = subPeriod(p, b)
		out = append(out, cell)
	}
	return out, rows.Err()
}
