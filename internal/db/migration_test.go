package db

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestUserKeyMigrationDisablesLegacyBackground(t *testing.T) {
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
	const schema = "byok_migration_test"
	if _, err = admin.Exec(ctx, `CREATE SCHEMA `+schema); err != nil {
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
	if _, err = pool.Exec(ctx, `CREATE TABLE schema_migrations(version TEXT PRIMARY KEY,applied_at TIMESTAMPTZ NOT NULL DEFAULT now())`); err != nil {
		t.Fatal(err)
	}
	entries, err := migrationFiles.ReadDir("migrations")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if name >= "0006" {
			continue
		}
		sql, err := migrationFiles.ReadFile("migrations/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = pool.Exec(ctx, string(sql)); err != nil {
			t.Fatal(err)
		}
		if _, err = pool.Exec(ctx, `INSERT INTO schema_migrations(version) VALUES($1)`, strings.TrimSuffix(name, ".sql")); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = pool.Exec(ctx, `INSERT INTO user_settings(user_id,background_categorization) VALUES('local',true)`); err != nil {
		t.Fatal(err)
	}
	if err = Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	var enabled, hasKey bool
	var model string
	var spacing, concurrency, batch int
	err = pool.QueryRow(ctx, `SELECT background_categorization,openrouter_key IS NOT NULL,ai_model,ai_interval_ms,ai_concurrency,ai_batch_size FROM user_settings WHERE user_id='local'`).Scan(&enabled, &hasKey, &model, &spacing, &concurrency, &batch)
	if err != nil || enabled || hasKey || model != "deepseek/deepseek-v4.1-flash" || spacing != 250 || concurrency != 8 || batch != 15 {
		t.Fatalf("unsafe upgrade: enabled=%t key=%t model=%q err=%v", enabled, hasKey, model, err)
	}
	if err = Migrate(ctx, pool); err != nil {
		t.Fatal("migrations not idempotent", err)
	}
}
