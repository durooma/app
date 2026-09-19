package store

import (
	"context"
	"time"

	"durooma/internal/models"
)

// DueCategorization returns a small page, oldest eligible work first. A failed
// item is delayed independently, so it cannot starve newly imported work.
func (s *Store) DueCategorization(ctx context.Context, limit int) ([]models.Transaction, error) {
	rows, err := s.pool.Query(ctx, txnSelect+`
        WHERE t.category_id IS NULL AND t.categorize_after <= now()
        ORDER BY t.categorize_after, t.id LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.Transaction
	for rows.Next() {
		t, err := scanTxn(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// DeferCategorization persists retries only for work still unresolved. Successful
// rows naturally leave the queue. Backoff grows from five minutes to one day.
func (s *Store) DeferCategorization(ctx context.Context, ids []int64) error {
	_, err := s.pool.Exec(ctx, `UPDATE transactions
        SET categorize_after = now() + LEAST(86400, 300 * power(2, LEAST(categorize_attempts, 9))) * interval '1 second',
            categorize_attempts = LEAST(categorize_attempts + 1, 10)
        WHERE id = ANY($1) AND category_id IS NULL`, ids)
	return err
}

// PendingTransactions refreshes a queued snapshot after waiting for another run.
func (s *Store) PendingTransactions(ctx context.Context, ids []int64) ([]models.Transaction, error) {
	rows, err := s.pool.Query(ctx, txnSelect+` WHERE t.id = ANY($1) AND t.category_id IS NULL ORDER BY t.id`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.Transaction
	for rows.Next() {
		t, err := scanTxn(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// SetUncategorizedCategory cannot overwrite a user's edit made during an API call.
func (s *Store) SetUncategorizedCategory(ctx context.Context, id int64, categoryID *int64) error {
	_, err := s.pool.Exec(ctx, `UPDATE transactions SET category_id = $2
        WHERE id = $1 AND category_id IS NULL`, id, categoryID)
	return err
}

// ReserveAIRequest atomically reserves an immediate request, or returns how long
// to wait. Only actual reservations advance the persistent clock.
func (s *Store) ReserveAIRequest(ctx context.Context, spacing time.Duration) (time.Duration, error) {
	tag, err := s.pool.Exec(ctx, `UPDATE ai_request_budget
        SET next_request_at = now() + $1 * interval '1 second'
        WHERE id AND next_request_at <= now()`, spacing.Seconds())
	if err != nil {
		return 0, err
	}
	if tag.RowsAffected() == 1 {
		return 0, nil
	}
	var seconds float64
	err = s.pool.QueryRow(ctx, `SELECT GREATEST(0.001, EXTRACT(EPOCH FROM next_request_at - now()))
        FROM ai_request_budget WHERE id`).Scan(&seconds)
	return time.Duration(seconds * float64(time.Second)), err
}

// FinishAIRequest preserves quota delays across restarts and manual runs.
func (s *Store) FinishAIRequest(ctx context.Context, failed bool, retryAfter time.Duration) error {
	if !failed {
		_, err := s.pool.Exec(ctx, `UPDATE ai_request_budget SET failures = 0 WHERE id`)
		return err
	}
	_, err := s.pool.Exec(ctx, `UPDATE ai_request_budget
        SET next_request_at = GREATEST(next_request_at,
            now() + GREATEST($1, LEAST(86400, 60 * power(2, LEAST(failures, 11)))) * interval '1 second'),
            failures = LEAST(failures + 1, 12) WHERE id`, retryAfter.Seconds())
	return err
}
