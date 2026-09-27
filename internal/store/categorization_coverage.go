package store

import "context"

// CategorizationCoverage counts assigned categories regardless of whether they
// came from a manual edit, a rule, or an AI request.
type CategorizationCoverage struct {
	Total             int
	Categorized       int
	CategoriesChanged bool
}

func (c CategorizationCoverage) Remaining() int { return c.Total - c.Categorized }

func (c CategorizationCoverage) Percent() float64 {
	if c.Total == 0 {
		return 0
	}
	return 100 * float64(c.Categorized) / float64(c.Total)
}

// CategorizationCoverage reads both counts in one snapshot so imports and
// background writes cannot produce inconsistent totals between queries.
func (s *Store) CategorizationCoverage(ctx context.Context) (CategorizationCoverage, error) {
	var coverage CategorizationCoverage
	err := s.pool.QueryRow(ctx, `SELECT count(*), count(category_id),
        (SELECT COALESCE(run_revision <> category_revision, false) FROM categorization_state WHERE id)
        FROM transactions`).Scan(&coverage.Total, &coverage.Categorized, &coverage.CategoriesChanged)
	return coverage, err
}
