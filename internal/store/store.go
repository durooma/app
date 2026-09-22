// Package store contains all SQL data access. Queries are hand-written against
// pgx for a small, dependency-light footprint (no ORM).
package store

import (
	"durooma/internal/secrets"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct {
	pool  *pgxpool.Pool
	vault *secrets.Vault
}

func New(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

func (s *Store) Pool() *pgxpool.Pool { return s.pool }

func isNoRows(err error) bool {
	return errors.Is(err, pgx.ErrNoRows)
}

// NewWithVault enables encrypted user credentials. New remains useful for stores
// that never access credentials (imports, reports, tests).
func NewWithVault(pool *pgxpool.Pool, vault *secrets.Vault) *Store {
	return &Store{pool: pool, vault: vault}
}
