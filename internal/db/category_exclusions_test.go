package db

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestCategoryExclusionMigrationPreservesExistingTransfer(t *testing.T) {
	databaseURL := os.Getenv("DUROOMA_TEST_DB")
	if databaseURL == "" {
		t.Skip("set DUROOMA_TEST_DB for migration integration tests")
	}
	ctx := context.Background()
	admin, err := Connect(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	const schema = "category_exclusion_migration_test"
	if _, err := admin.Exec(ctx, `CREATE SCHEMA `+schema); err != nil {
		t.Fatal(err)
	}
	defer admin.Exec(ctx, `DROP SCHEMA `+schema+` CASCADE`)
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	initial, err := migrationFiles.ReadFile("migrations/0001_init.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, string(initial)); err != nil {
		t.Fatal(err)
	}
	var id int64
	if err := pool.QueryRow(ctx, `INSERT INTO categories(name, description, sort_order) VALUES ('transfer', 'My existing description', 42) RETURNING id`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO rules(pattern, category_id) VALUES ('own account', $1)`, id); err != nil {
		t.Fatal(err)
	}
	migration, err := migrationFiles.ReadFile("migrations/0007_category_exclusions.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, string(migration)); err != nil {
		t.Fatal(err)
	}
	var count, order int
	var gotID int64
	var name, description string
	var excluded bool
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM categories`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("duplicate Transfer category: %d, %v", count, err)
	}
	if err := pool.QueryRow(ctx, `SELECT id, name, description, sort_order, exclude_from_calculations FROM categories`).Scan(&gotID, &name, &description, &order, &excluded); err != nil {
		t.Fatal(err)
	}
	if gotID != id || name != "transfer" || description != "My existing description" || order != 42 || !excluded {
		t.Fatal("migration changed existing category metadata or failed to exclude it")
	}
	if err := pool.QueryRow(ctx, `SELECT category_id FROM rules`).Scan(&gotID); err != nil || gotID != id {
		t.Fatalf("migration broke rule assignment: %v", err)
	}
}
