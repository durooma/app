package store

import (
	"context"
	"errors"
)

// BeginCategorization records the baseline before reading category definitions.
// Keep it across incremental runs: processing only newly imported transactions
// cannot make older assignments consistent with edited categories.
func (s *Store) BeginCategorization(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, `UPDATE categorization_state SET run_revision=category_revision WHERE id AND run_revision IS NULL`)
	return err
}

// ResetCategorization must run under the AI service's exclusive reset gate.
// Deletion, retry reset, baseline and background opt-in commit together.
func (s *Store) ResetCategorization(ctx context.Context, userID string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var hasKey bool
	err = tx.QueryRow(ctx, `SELECT openrouter_key IS NOT NULL FROM user_settings WHERE user_id=$1 FOR UPDATE`, userID).Scan(&hasKey)
	if isNoRows(err) || err == nil && !hasKey {
		return errors.New("save an OpenRouter API key before resetting categorization")
	}
	if err != nil {
		return err
	}
	// Freeze category revision updates until all old assignments are cleared.
	if _, err = tx.Exec(ctx, `UPDATE categorization_state SET run_revision=NULL WHERE id`); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE transactions SET category_id=NULL, categorize_attempts=0, categorize_after=now()`); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE user_settings SET background_categorization=true, ai_revision=ai_revision+1 WHERE user_id=$1`, userID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
