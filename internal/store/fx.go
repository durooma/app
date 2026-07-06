package store

import (
	"context"
	"time"
)

// CachedRate returns a previously stored FX rate (base->quote on date), if any.
func (s *Store) CachedRate(ctx context.Context, date time.Time, base, quote string) (float64, bool, error) {
	var rate float64
	err := s.pool.QueryRow(ctx, `
		SELECT rate FROM exchange_rates WHERE rate_date = $1 AND base = $2 AND quote = $3`,
		date, base, quote).Scan(&rate)
	if err != nil {
		if isNoRows(err) {
			return 0, false, nil
		}
		return 0, false, err
	}
	return rate, true, nil
}

// StoreRate caches an FX rate for reuse.
func (s *Store) StoreRate(ctx context.Context, date time.Time, base, quote string, rate float64) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO exchange_rates (rate_date, base, quote, rate)
		VALUES ($1,$2,$3,$4)
		ON CONFLICT (rate_date, base, quote) DO UPDATE SET rate = EXCLUDED.rate`,
		date, base, quote, rate)
	return err
}

func (s *Store) GetSetting(ctx context.Context, key string) (string, bool, error) {
	var v string
	err := s.pool.QueryRow(ctx, `SELECT value FROM settings WHERE key = $1`, key).Scan(&v)
	if err != nil {
		if isNoRows(err) {
			return "", false, nil
		}
		return "", false, err
	}
	return v, true, nil
}

func (s *Store) SetSetting(ctx context.Context, key, value string) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO settings (key, value) VALUES ($1,$2)
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value`, key, value)
	return err
}
