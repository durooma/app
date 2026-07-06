package store

import (
	"context"

	"durooma/internal/models"
)

func (s *Store) ListCategories(ctx context.Context) ([]models.Category, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, name, description, COALESCE(kind, ''), sort_order
		FROM categories
		ORDER BY sort_order, name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []models.Category
	for rows.Next() {
		var c models.Category
		if err := rows.Scan(&c.ID, &c.Name, &c.Description, &c.Kind, &c.SortOrder); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) GetCategory(ctx context.Context, id int64) (models.Category, error) {
	var c models.Category
	err := s.pool.QueryRow(ctx, `
		SELECT id, name, description, COALESCE(kind, ''), sort_order
		FROM categories WHERE id = $1`, id).
		Scan(&c.ID, &c.Name, &c.Description, &c.Kind, &c.SortOrder)
	return c, err
}

func (s *Store) CreateCategory(ctx context.Context, c models.Category) (int64, error) {
	var id int64
	err := s.pool.QueryRow(ctx, `
		INSERT INTO categories (name, description, kind, sort_order)
		VALUES ($1, $2, $3, $4) RETURNING id`,
		c.Name, c.Description, kindOrNil(c.Kind), c.SortOrder).Scan(&id)
	return id, err
}

func (s *Store) UpdateCategory(ctx context.Context, c models.Category) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE categories SET name = $2, description = $3, kind = $4, sort_order = $5
		WHERE id = $1`,
		c.ID, c.Name, c.Description, kindOrNil(c.Kind), c.SortOrder)
	return err
}

func (s *Store) DeleteCategory(ctx context.Context, id int64) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM categories WHERE id = $1`, id)
	return err
}

// CategoryByName resolves a category id by exact name (used by importers/AI).
func (s *Store) CategoryByName(ctx context.Context, name string) (int64, bool, error) {
	var id int64
	err := s.pool.QueryRow(ctx, `SELECT id FROM categories WHERE lower(name) = lower($1)`, name).Scan(&id)
	if err != nil {
		if isNoRows(err) {
			return 0, false, nil
		}
		return 0, false, err
	}
	return id, true, nil
}

func kindOrNil(kind string) any {
	if kind == "income" || kind == "expense" {
		return kind
	}
	return nil
}
