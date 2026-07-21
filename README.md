# Durooma — self-hosted personal finance

A lean personal-finance app that unifies transactions across accounts,
institutions and currencies, with yearly/monthly deep dives, month-range
amortization, CSV imports (UBS Switzerland & Charles Schwab) and AI-assisted
categorization.

Built to run comfortably on a **512 MB** DigitalOcean droplet or a home server:
a single ~15 MB Go binary (net/http + `html/template` + HTMX) backed by
PostgreSQL. Two runtime dependencies, no Node build step, no ORM.

## Features

- **Editable categories** with name, description and optional income/expense type.
- **Unified transaction view** across every account/institution/currency, with
  filtering (institution, account, category, uncategorized, date range, search).
- **Yearly deep dive** — income & expenses by category, plus a monthly breakdown.
- **Monthly deep dive** — one month, by category.
- **Year overview** — every category across all 12 months in one grid.
- **Multi-year overview** — income/expense/net per year and categories across years.
- **Month-range assignment / amortization** — a transaction defaults to the month
  it occurred, but can be reassigned to another month or spread across a range
  (quarter, year, …). Amortized amounts are divided evenly across the months in
  every report.
- **Multi-currency** — each account has its own currency; amounts are converted to
  your base currency (default `CHF`) using historical ECB rates
  ([frankfurter.app](https://frankfurter.app)), cached in the database.
- **CSV import** for UBS Switzerland plus Charles Schwab brokerage and Equity
  Awards exports, with dedup by a date/description/amount hash.
- **Auto-categorization** — deterministic substring rules first, then a pluggable
  LLM provider (Gemini by default) for the rest.

## Quick start (Docker)

```sh
cp .env.example .env        # set POSTGRES_PASSWORD, AI_API_KEY, etc.
docker compose up --build -d
# open http://localhost:8080
```

Postgres is capped at 256 MB and the app at 128 MB in `docker-compose.yml`, so
the whole stack fits in a 512 MB box with headroom.

## Local development

```sh
make db      # starts a throwaway Postgres on :5432 (separate terminal)
make run     # runs the app; migrations apply automatically on boot
make test    # unit tests

# Integration tests need a database:
DUROOMA_TEST_DB=postgres://durooma:durooma@localhost:5433/durooma?sslmode=disable \
  go test ./internal/store/ ./internal/importer/
```

## Configuration (environment variables)

| Variable | Default | Purpose |
|---|---|---|
| `DATABASE_URL` | `postgres://durooma:durooma@localhost:5432/durooma?sslmode=disable` | Postgres connection |
| `HTTP_ADDR` | `:8080` | Listen address |
| `BASE_CURRENCY` | `CHF` | Currency all reports normalise to |
| `AI_PROVIDER` | `gemini` | `gemini` or `none` |
| `AI_MODEL` | `gemini-3.1-flash-lite` | Model id |
| `AI_API_KEY` | — | LLM API key (required if AI enabled) |
| `FX_BASE_URL` | `https://api.frankfurter.app` | Historical FX rate source |

## How importing works

1. Upload a UBS or Schwab CSV on the **Import** page (or `POST /import`).
2. The matching parser extracts transactions; foreign-currency amounts are
   converted to the base currency and the account is auto-created.
3. Duplicates (same date + description + amount) are skipped.
4. Run **Auto-categorize** to apply rules, then the AI provider, to anything
   still uncategorized.

Schwab Equity Awards imports treat vested RS shares as income at vest fair-market
value. A later sale contributes only its lot-based gain or loss (after fees), not
the full proceeds. Compact Schwab brokerage exports do not contain lot cost basis,
so stock-plan sale proceeds are skipped there and handled by the Equity Awards
export; other signed `Amount` values are imported as provided. Enter an account
name when importing separate Individual and Joint Tenant files to keep them
distinct.

## Architecture

```
cmd/server            entrypoint (config, DB, migrations, graceful shutdown)
internal/config       env-based configuration
internal/db           pgx pool + embedded SQL migrations (no external tooling)
internal/models       domain types
internal/store        hand-written SQL (categories, accounts, transactions, reports…)
internal/fx           currency conversion + rate caching
internal/importer     UBS + Schwab CSV parsers and the import orchestrator
internal/ai           provider-agnostic categorization (Gemini included)
internal/web          net/http router, html/template pages, embedded static assets
```

The amortization that powers every report lives in `internal/store/reports.go`:
a Postgres `generate_series` expands each transaction into one row per month in
its window, dividing the amount evenly — so a yearly insurance premium shows up
as 1/12 in each month automatically.

## Relationship to the original Google Sheets script

This app is a port and expansion of the previous Apps Script:

| Apps Script | Here |
|---|---|
| `parseUBS` / `parseSchwab` | `internal/importer/ubs.go`, `schwab.go` |
| `generateID` dedup | `importer.GenerateHash` + `external_hash UNIQUE` |
| `GOOGLEFINANCE` CHF conversion | `internal/fx` (cached ECB rates) |
| Rules sheet + Gemini batching | `internal/ai` (rules pass + Gemini provider) |
| Start/End month columns | `transactions.start_month` / `end_month` + amortization |
