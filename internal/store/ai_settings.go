package store

import (
	"context"
	"errors"
	"strings"
	"time"

	"durooma/internal/models"
)

func (s *Store) AISettings(ctx context.Context, userID string) (models.AISettings, error) {
	out := models.DefaultAISettings()
	err := s.pool.QueryRow(ctx, `SELECT openrouter_key IS NOT NULL, background_categorization,
 ai_model,ai_interval_ms,ai_concurrency,ai_batch_size,ai_revision FROM user_settings WHERE user_id=$1`, userID).
		Scan(&out.HasKey, &out.Background, &out.Model, &out.IntervalMS, &out.Concurrency, &out.BatchSize, &out.Revision)
	if isNoRows(err) {
		err = nil
	}
	return out, err
}

func (s *Store) OpenRouterKey(ctx context.Context, userID string) (string, error) {
	var encrypted []byte
	err := s.pool.QueryRow(ctx, `SELECT openrouter_key FROM user_settings WHERE user_id=$1`, userID).Scan(&encrypted)
	if isNoRows(err) || err == nil && len(encrypted) == 0 {
		return "", nil
	}
	if err != nil {
		return "", errors.New("could not load API credentials")
	}
	if s.vault == nil {
		return "", errors.New("credential encryption is unavailable")
	}
	return s.vault.Open(userID, encrypted)
}

// SaveAISettings changes preferences and credentials atomically. Empty key keeps
// the current credential. Removal is explicit and always disables background work.
func (s *Store) SaveAISettings(ctx context.Context, userID string, settings models.AISettings, key string, remove bool) error {
	if err := settings.Validate(); err != nil {
		return err
	}
	if s.vault == nil {
		return errors.New("credential encryption is unavailable")
	}
	if key != "" && (len(key) < 16 || len(key) > 512 || strings.IndexFunc(key, func(r rune) bool { return r < 33 || r > 126 }) >= 0) {
		return errors.New("enter a valid OpenRouter API key")
	}
	if remove && key != "" {
		return errors.New("choose either replace or remove API key")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `INSERT INTO user_settings(user_id) VALUES($1) ON CONFLICT DO NOTHING`, userID); err != nil {
		return err
	}
	var encrypted []byte
	if err = tx.QueryRow(ctx, `SELECT openrouter_key FROM user_settings WHERE user_id=$1 FOR UPDATE`, userID).Scan(&encrypted); err != nil {
		return err
	}
	if remove {
		encrypted = nil
		settings.Background = false
	} else if key != "" {
		encrypted = s.vault.Seal(userID, key)
	}
	if settings.Background && len(encrypted) == 0 {
		return errors.New("save your own OpenRouter API key before enabling background categorization")
	}
	_, err = tx.Exec(ctx, `UPDATE user_settings SET openrouter_key=$2,background_categorization=$3,
 ai_model=$4,ai_interval_ms=$5,ai_concurrency=$6,ai_batch_size=$7,ai_revision=ai_revision+1 WHERE user_id=$1`, userID, encrypted, settings.Background, settings.Model, settings.IntervalMS, settings.Concurrency, settings.BatchSize)
	if err != nil {
		return err
	}
	// Keep quota cooldowns when editing preferences, but a replacement key starts
	// a new budget. Old in-flight calls carry the revision and cannot modify it.
	if key != "" || remove {
		_, err = tx.Exec(ctx, `DELETE FROM user_ai_request_budget WHERE user_id=$1`, userID)
		if err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (s *Store) ReserveUserAIRequest(ctx context.Context, userID string, revision int64, spacing time.Duration) (time.Duration, error) {
	_, err := s.pool.Exec(ctx, `INSERT INTO user_ai_request_budget(user_id) VALUES($1) ON CONFLICT DO NOTHING`, userID)
	if err != nil {
		return 0, err
	}
	tag, err := s.pool.Exec(ctx, `UPDATE user_ai_request_budget b SET next_request_at=now()+$3*interval '1 second'
 FROM user_settings s WHERE b.user_id=$1 AND s.user_id=b.user_id AND s.ai_revision=$2 AND s.openrouter_key IS NOT NULL AND b.next_request_at<=now()`, userID, revision, spacing.Seconds())
	if err != nil {
		return 0, err
	}
	if tag.RowsAffected() == 1 {
		return 0, nil
	}
	var seconds float64
	err = s.pool.QueryRow(ctx, `SELECT GREATEST(0.001,EXTRACT(EPOCH FROM b.next_request_at-now())) FROM user_ai_request_budget b
 JOIN user_settings s USING(user_id) WHERE b.user_id=$1 AND s.ai_revision=$2 AND s.openrouter_key IS NOT NULL`, userID, revision).Scan(&seconds)
	if isNoRows(err) {
		return 0, errors.New("categorization settings changed; start a new run")
	}
	return time.Duration(seconds * float64(time.Second)), err
}
func (s *Store) FinishUserAIRequest(ctx context.Context, userID string, revision int64, failed bool, retryAfter time.Duration) error {
	if !failed {
		_, err := s.pool.Exec(ctx, `UPDATE user_ai_request_budget b SET failures=0 FROM user_settings s WHERE b.user_id=$1 AND s.user_id=b.user_id AND s.ai_revision=$2`, userID, revision)
		return err
	}
	_, err := s.pool.Exec(ctx, `UPDATE user_ai_request_budget b SET next_request_at=GREATEST(b.next_request_at,
 now()+GREATEST($3,LEAST(30,2*power(2,LEAST(b.failures,4))))*interval '1 second'),failures=LEAST(b.failures+1,5)
 FROM user_settings s WHERE b.user_id=$1 AND s.user_id=b.user_id AND s.ai_revision=$2`, userID, revision, retryAfter.Seconds())
	return err
}
