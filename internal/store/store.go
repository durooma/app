// Package store contains all SQL data access. Queries are hand-written against
// pgx for a small, dependency-light footprint (no ORM).
package store

import (
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

func (s *Store) Pool() *pgxpool.Pool { return s.pool }

func isNoRows(err error) bool {
	return errors.Is(err, pgx.ErrNoRows)
}
