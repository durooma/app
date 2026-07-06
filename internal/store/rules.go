package store

import (
	"context"

	"durooma/internal/models"
)

func (s *Store) ListRules(ctx context.Context) ([]models.Rule, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT r.id, r.pattern, r.category_id, c.name, r.priority
		FROM rules r JOIN categories c ON c.id = r.category_id
		ORDER BY r.priority DESC, r.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.Rule
	for rows.Next() {
		var r models.Rule
		if err := rows.Scan(&r.ID, &r.Pattern, &r.CategoryID, &r.CategoryName, &r.Priority); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) CreateRule(ctx context.Context, pattern string, categoryID int64, priority int) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO rules (pattern, category_id, priority) VALUES ($1,$2,$3)`,
		pattern, categoryID, priority)
	return err
}

func (s *Store) DeleteRule(ctx context.Context, id int64) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM rules WHERE id = $1`, id)
	return err
}
