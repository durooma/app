package store

import "context"

// LocalUserID identifies the owner of this single-user installation. It must not
// come from a request parameter: the app has no authentication/user switching.
const LocalUserID = "local"

// BackgroundCategorizationEnabled defaults to off for users without preferences.
func (s *Store) BackgroundCategorizationEnabled(ctx context.Context, userID string) (bool, error) {
	var enabled bool
	err := s.pool.QueryRow(ctx, `SELECT COALESCE((SELECT background_categorization
        FROM user_settings WHERE user_id = $1), false)`, userID).Scan(&enabled)
	return enabled, err
}

func (s *Store) SetBackgroundCategorization(ctx context.Context, userID string, enabled bool) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO user_settings (user_id, background_categorization)
        VALUES ($1, $2) ON CONFLICT (user_id) DO UPDATE
        SET background_categorization = EXCLUDED.background_categorization`, userID, enabled)
	return err
}
