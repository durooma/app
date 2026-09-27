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
  user-configured OpenRouter model for the rest (DeepSeek V4.1 Flash by default).
  Your own API key is required. An optional persistent background
  worker picks up uncategorized transactions with paced requests and retries.
  Enable it in **Settings**; it defaults to off.

## Quick start (Docker)

```sh
cp .env.example .env        # set POSTGRES_PASSWORD, etc.
docker compose up --build -d
# open http://localhost:8080, then add your OpenRouter key in Settings
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
  go test -p 1 ./internal/store/ ./internal/importer/ ./internal/web/
```

## Configuration (environment variables)

| Variable | Default | Purpose |
|---|---|---|
| `DATABASE_URL` | `postgres://durooma:durooma@localhost:5432/durooma?sslmode=disable` | Postgres connection |
| `HTTP_ADDR` | `:8080` | Listen address |
| `BASE_CURRENCY` | `CHF` | Currency all reports normalise to |
| `CREDENTIALS_KEY_FILE` | `.durooma/credentials.key` | Private server encryption key; created on first startup. Docker uses the persistent `app-secrets` volume. |
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

## OpenRouter credentials and categorization

In **Settings → Categorization**, save your own OpenRouter API key. Both manual
and background auto-categorization require it; ordinary manual category edits
and imports remain available without a key. A saved key appears as a masked list
item. Its **×** button immediately removes the key, turns background processing
off, and restores the key input so a replacement can be saved. Other saved
preferences are preserved. Keys are never returned to the browser. Requests use
OpenRouter with `zdr: true` and `data_collection: "deny"`; a model without an
eligible endpoint fails rather than relaxing these privacy constraints.

The default model is `deepseek/deepseek-v4.1-flash`. The model selector also offers
GLM 5.3 Flash and Gemini 3.1 Flash Lite through OpenRouter, plus a custom model ID.
Each preset shows an illustrative USD cost per 1,000 transactions at batch size
15, using a shared workload of 46,000 input and 64,000 output tokens (including
reasoning), rounded from the GLM eval. The expanded cost explanation links the
dated reference prices; estimates are not measurements for every model, live
quotes, or spending limits. Actual token usage and ZDR provider rates can differ.
Users can change the model,
minimum request spacing (**250 ms**, up to 4 requests/second), concurrency
(**8**, range 1–32), and batch size (**15**, range 1–100) in Settings. There is no
application daily request cap. These settings share a durable per-owner pacing
budget across manual runs, background work and retries. Provider rate limits
still apply; request limits are not spending limits. Saving settings cancels
active categorization runs; completed categories remain, and the next run uses
the new settings. Manual runs are serialized with background runs to avoid
processing the same transactions twice; provider calls within a run are parallel.
Run one app instance per database.

Background processing is off by default and can only be enabled with a saved
key. It discovers existing uncategorized transactions and new imports, polling
every 30 seconds. The Settings coverage indicator updates every five seconds,
including while background processing is off. **Reset and recategorize** opens
a confirmation page: it clears all transaction category assignments (including
manual ones), clears transaction retry delays, and enables background processing
to start again with the saved key and model. Transactions, category definitions,
and rules remain. Active requests are stopped before clearing assignments;
the reset cannot be undone and fresh provider requests incur normal charges.

Settings recommends a reset when categories have been added, changed, or deleted
since the categorization baseline. The warning persists across incremental runs
because existing assignments are not revisited. A reset establishes a fresh
baseline on the next run. Change tracking begins when this feature is installed;
it cannot detect category edits made before the migration.

Transient HTTP 408/429/5xx and network failures retry up to five attempts with a
shared exponential cooldown (2–30 seconds, or longer for `Retry-After`). Each
attempt has a 120-second timeout. Invalid model outputs are not retried; they
remain uncategorized. Unresolved transactions have a persisted retry delay of
5 minutes up to 24 hours. Existing categories, including concurrent manual edits,
are never overwritten. Aborting a manual run preserves completed assignments;
an enabled background worker may later pick up remaining work.

### Credential storage and deployment

API keys are encrypted with AES-256-GCM using fresh random nonces and the owner
ID as authenticated context. Only ciphertext goes into Postgres. The server's
32-byte encryption key is stored separately in an owner-only (0600) file, created
atomically on first startup. Docker runs as the unprivileged `app` user and keeps
this file in the **`app-secrets` volume**. Local runs use the gitignored
`.durooma/credentials.key`; set `CREDENTIALS_KEY_FILE` to use another protected
location. No provider key or encryption key is bundled into the image.

Back up the server encryption key separately from the database. Keep the same
key file when moving or restoring an installation. Losing it makes existing API
keys unreadable; a mismatched key causes startup to fail rather than silently
using another credential. Treat access to both the database and server key as
access to the API credentials. Restrict database/backup and host access.

This remains a **single-user installation without built-in login**. The `local`
owner is selected by the server, never from a form or query parameter; all browsers
that can access the app share that owner's settings. For network access, place
the app behind an authenticated HTTPS reverse proxy or VPN. Browser POST requests
have cross-origin protection; this does not replace authentication. Settings
responses are marked `no-store`, submitted keys are never reflected, and provider
errors do not contain request payloads or credentials.

### Upgrading from the shared API key

`AI_API_KEY`, `AI_PROVIDER`, `AI_MODEL`, `AI_BATCH_SIZE`, `AI_REQUESTS_PER_DAY`,
and `AI_REQUEST_INTERVAL` are deprecated and **ignored** by the server. They are
not copied into user settings and never act as a fallback. The migration disables
the old background preference; add your own key and opt in again through Settings.
Remove obsolete values from deployment configuration. `OPENROUTER_API_KEY` in
`.eval-env` remains exclusively for the separate eval CLI.

## Categorization evals

Compare models, prompt templates, and batch sizes against a fixed labeled
dataset with `go run ./cmd/eval`. Start with `go run ./cmd/eval -dry-run` to
validate the combined 770-case suite and see the request count without API
calls. The eval configs use OpenRouter with 8 workers, 250ms request spacing
and retries for transient failures. The runner saves overall and per-source scores,
confidence slices, failures, latency, and individual predictions without
connecting to the database or changing categories.
See [the eval guide](evals/README.md) for model configuration, reviewed datasets,
pacing, and interpreting results.

## Architecture

```
cmd/server            entrypoint (config, DB, migrations, graceful shutdown)
cmd/eval              categorization comparison CLI
internal/eval         labeled datasets, experiment matrix, scoring, reports
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
