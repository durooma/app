package store

import (
	"context"

	"durooma/internal/models"
)

func (s *Store) ListInstitutions(ctx context.Context) ([]models.Institution, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, name, created_at FROM institutions ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.Institution
	for rows.Next() {
		var i models.Institution
		if err := rows.Scan(&i.ID, &i.Name, &i.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, rows.Err()
}

// UpsertInstitution returns the id of an institution, creating it if needed.
func (s *Store) UpsertInstitution(ctx context.Context, name string) (int64, error) {
	var id int64
	err := s.pool.QueryRow(ctx, `
		INSERT INTO institutions (name) VALUES ($1)
		ON CONFLICT (name) DO UPDATE SET name = EXCLUDED.name
		RETURNING id`, name).Scan(&id)
	return id, err
}

func (s *Store) ListAccounts(ctx context.Context) ([]models.Account, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT a.id, a.institution_id, i.name, a.name, a.currency, a.created_at
		FROM accounts a
		JOIN institutions i ON i.id = a.institution_id
		ORDER BY i.name, a.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.Account
	for rows.Next() {
		var a models.Account
		if err := rows.Scan(&a.ID, &a.InstitutionID, &a.InstitutionName, &a.Name, &a.Currency, &a.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// UpsertAccount finds or creates an account under an institution (by name).
func (s *Store) UpsertAccount(ctx context.Context, institutionID int64, name, currency string) (int64, error) {
	var id int64
	err := s.pool.QueryRow(ctx, `
		INSERT INTO accounts (institution_id, name, currency)
		VALUES ($1, $2, $3)
		ON CONFLICT (institution_id, name) DO UPDATE SET currency = EXCLUDED.currency
		RETURNING id`, institutionID, name, currency).Scan(&id)
	return id, err
}

func (s *Store) CreateAccount(ctx context.Context, institutionName, accountName, currency string) (int64, error) {
	instID, err := s.UpsertInstitution(ctx, institutionName)
	if err != nil {
		return 0, err
	}
	return s.UpsertAccount(ctx, instID, accountName, currency)
}
