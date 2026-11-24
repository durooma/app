package store

import "context"

// CategorizationCoverage counts assigned categories regardless of whether they
// came from a manual edit, a rule, or an AI request, and likewise decided
// periods, whether picked by hand or suggested.
type CategorizationCoverage struct {
	Total             int
	Categorized       int
	CategoriesChanged bool

	PeriodsDecided int
	// PeriodsMoved counts decided periods other than the transaction's own
	// month: spread over a quarter or year, or shifted to another month.
	PeriodsMoved int
}

func (c CategorizationCoverage) Remaining() int { return c.Total - c.Categorized }

func (c CategorizationCoverage) Percent() float64 { return percentOf(c.Categorized, c.Total) }

func (c CategorizationCoverage) PeriodsRemaining() int { return c.Total - c.PeriodsDecided }

func (c CategorizationCoverage) PeriodsPercent() float64 { return percentOf(c.PeriodsDecided, c.Total) }

func percentOf(n, total int) float64 {
	if total == 0 {
		return 0
	}
	return 100 * float64(n) / float64(total)
}

// CategorizationCoverage reads both counts in one snapshot so imports and
// background writes cannot produce inconsistent totals between queries.
func (s *Store) CategorizationCoverage(ctx context.Context) (CategorizationCoverage, error) {
	var coverage CategorizationCoverage
	err := s.pool.QueryRow(ctx, `SELECT count(*), count(category_id),
        (SELECT COALESCE(run_revision <> category_revision, false) FROM categorization_state WHERE id),
        count(period_source),
        count(*) FILTER (WHERE period_source IS NOT NULL
            AND (start_month <> date_trunc('month', txn_date)::date OR end_month <> start_month))
        FROM transactions`).Scan(&coverage.Total, &coverage.Categorized, &coverage.CategoriesChanged,
		&coverage.PeriodsDecided, &coverage.PeriodsMoved)
	return coverage, err
}
