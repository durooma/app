# Durooma — self-hosted personal finance

A lean personal-finance app that unifies transactions across accounts,
institutions and currencies, with a drill-down report from all time down to a
single month, month-range amortization, CSV imports (UBS Switzerland & Charles
Schwab) and AI-assisted categorization.

Built to run comfortably on a **512 MB** DigitalOcean droplet or a home server:
a single ~15 MB Go binary (net/http + `html/template` + HTMX) backed by
PostgreSQL. Two runtime dependencies, no Node build step, no ORM.

## Features

- **Editable categories** with name, description and optional income/expense type.
- **Unified transaction view** across every account/institution/currency, with
  filtering (institution, account, category, uncategorized, date range, search).
- **One report view, drilled by scope** — `/reports` opens on all time, showing
  income, expenses and balance, the same split by category (with a column per
  year), and a per-year list. Clicking a year re-scopes everything to that year
  with a column and a row per month; clicking a month does the same and lists
  that month's transactions. Every figure links into the transactions view with
  the matching period, sign and category already filtered. Categories are split
  by sign rather than netted, so a refunded expense or a mixed uncategorized
  bucket shows on both sides and each table adds up to the headline total.
- **Month-range assignment / amortization** — a transaction defaults to the month
  it occurred, but can be reassigned to another month or spread across a range
  (quarter, year, …). Amortized amounts are divided evenly across the months in
  every report.
- **Multi-currency** — each account has its own currency; amounts are converted to
  your base currency (default `CHF`) using historical ECB rates
  ([frankfurter.app](https://frankfurter.app)), cached in the database.
- **Multi-file CSV import** for UBS Switzerland plus Charles Schwab brokerage and
  Equity Awards exports, with automatic format detection and dedup by a
  date/description/amount hash.
- **Auto-categorization** — deterministic substring rules first, then a pluggable
  LLM provider (Gemini by default) for the rest. An optional persistent background
  worker picks up uncategorized transactions with paced requests and retries.
  Enable it in **Settings**; it defaults to off.

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
make run     # loads .env and runs the app; migrations apply automatically on boot
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
| `AI_BATCH_SIZE` | `30` | Transactions grouped in each model request (1–100) |
| `AI_REQUESTS_PER_DAY` | `200` | Shared request allowance, evenly paced across 24 hours (1–86400) |
| `AI_REQUEST_INTERVAL` | `1m` | Minimum gap between requests; daily allowance may increase it |
| `AI_POLL_INTERVAL` | `30s` | How often the background worker looks for work |
| `FX_BASE_URL` | `https://api.frankfurter.app` | Historical FX rate source |

## How importing works

1. Upload one or more UBS or Schwab CSVs on the **Import** page (or `POST /import`
   with one or more `files` parts).
2. The matching parser is inferred from each file's headers and extracts its
   transactions; foreign-currency amounts are converted to the base currency and
   the account is auto-created.
3. Duplicates (same date + description + amount) are skipped.
4. Use **Categorize all** for a filtered manual run, or enable **Background
   categorization** in **Settings** to process transactions automatically.
   Rules run first, followed by grouped, paced model requests.

Each file is imported independently: an unreadable or unrecognized one is reported
by name and the rest still import. Limits per upload are 50 files, 16 MB per file
and 64 MB in total.

`POST /import` also accepts the pre-multi-file fields for compatibility: a single
`file` part, and a `provider` of `UBS` or `Schwab` that skips header detection and
is applied to every part.

Schwab Equity Awards imports treat vested RS shares as income at vest fair-market
value. A later sale contributes only its lot-based gain or loss (after fees), not
the full proceeds. Compact Schwab brokerage exports do not contain lot cost basis,
so stock-plan sale proceeds are skipped there and handled by the Equity Awards
export; other signed `Amount` values are imported as provided. Leave the account
name blank when importing separate Individual and Joint Tenant files so each is
named from its own header and they stay distinct — a name entered on the form
applies to every file in that upload and merges them into one account.

## Background categorization

Background categorization is **off by default**. Turn it on in **Settings →
Categorization**. This preference is saved in Postgres for the current user and
applies without a server restart. The app currently has one local user and no
login system; the preference belongs to that user across browsers. It does not
introduce multi-user authentication or separate transaction ownership.

While enabled, Settings shows categorization coverage: assigned categories out
of all transactions, the percentage covered, and how many remain. It refreshes
every five seconds and includes manual, rule-based, and AI assignments.

When enabled and an AI provider and API key are configured, the worker discovers
both existing uncategorized transactions and new imports. Postgres stores
per-transaction retry times and the shared next-request time, so restarting the server resumes pending work without
resetting the request budget. Run one app instance per database; the service
serializes automatic and manual runs within that instance.

Defaults allow one request every 7 minutes 12 seconds (200 spread over 24 hours),
with up to 30 transactions per request. Rule matches consume no API calls. This
is a local request budget, not a token or spending limit, and does not account
for other applications using the same API key. Set the budget to match your
provider quota. Increasing it still respects `AI_REQUEST_INTERVAL`.

Failed requests extend a shared exponential cooldown, honoring HTTP Retry-After.
Unresolved transactions get their own exponential retry delay (5 minutes up to
24 hours), letting other work proceed. Retry times and categories survive
restarts; an interrupted API request may be retried and billed again. Model
responses must contain one result per transaction. Automatic writes never
replace an already assigned category, including edits made while a call runs.

Manual **Categorize all** keeps its progress bar and Abort control and uses the
same budget. The single-transaction AI button also starts a background run so
quota waits do not hold a page request open. Only one manual run is active at a
time. It can take time while waiting for quota. Abort stops that manual run only; the automatic worker may pick up its remaining transactions later.
Turn **Background categorization** off in Settings to cancel automatic work,
including quota waits and in-flight requests. Categories already saved are kept,
and manual runs remain available. The worker also stays off with
`AI_PROVIDER=none` or a missing API key; a saved preference is retained until AI
is configured. Refresh a page to see completed categories; the automatic worker does not show a persistent progress bar.

Batching currently groups transactions in ordinary Gemini `generateContent`
requests. It does not submit jobs to the provider's separate asynchronous Batch
API. No real provider requests are needed to run the automated tests.

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
