# Auto-categorization eval

Run one benchmark through every **model × prompt × batch size** combination.
The default suite contains **770 examples**: 170 authored multilingual cases,
300 public synthetic cases and 300 AI-reviewed personal cases. The original
34 smoke cases are included in the 170 and are counted only once.

## Run

From the repository root:

```sh
# Validate everything and show the request count; no keys or API calls needed.
go run ./cmd/eval -dry-run

# OpenRouter: OPENROUTER_API_KEY must be exported.
go run ./cmd/eval

# Explicit comparison config: edit its models array to add OpenRouter models.
go run ./cmd/eval -config evals/config.compare.json

# Override the shared request pool if your quotas permit more concurrency.
go run ./cmd/eval -config evals/config.compare.json -concurrency 16

# A single dataset, or the original small smoke test.
go run ./cmd/eval -dataset evals/dataset.example.json -dry-run

# Export a self-contained snapshot while validating; destination must be new.
go run ./cmd/eval -dry-run -bundle evals/local/combined-v2.json
```

The command does not load environment files automatically. Keep eval credentials
in `.eval-env` and export only that file before running
(`set -a; . ./.eval-env; set +a`). Only `OPENROUTER_API_KEY` is required.
No production environment or Gemini API key is needed.
Credentials are referenced by environment-variable name and never saved in
configs or reports. No database access or transaction updates occur.

Both `evals/config.json` and `config.compare.json` currently use only
`google/gemini-3.1-flash-lite` through OpenRouter, with production and explicit
prompts at batch sizes 1, 15 and 30: **1,696 calls / 4,620 predictions** across
six variants. Add entries to `models` to compare other OpenRouter models. A
larger batch-only matrix makes far fewer requests. Request counts alone are
not monetary cost estimates. See the [current token/cost estimate](COST_ESTIMATE.md)
for this specific matrix.

```json
{"name":"router","provider":"openrouter","model":"google/gemini-3.1-flash-lite","api_key_env":"OPENROUTER_API_KEY"}
```

OpenRouter uses its [Chat Completions API](https://openrouter.ai/docs/quickstart)
with an explicit model slug, bearer authentication and a nonstreaming user
message. It checks both HTTP failures and error objects inside HTTP 200
responses. There is no client-side model fallback. Transient request failures are retried
using the policy below. OpenRouter can
route the selected model between upstream providers using its defaults; this
can affect latency and reproducibility. No search/tools are enabled for the
models being evaluated. Generation/sampling parameters use provider defaults.

## Dataset, provenance and reference labels

`evals/suite.json` resolves every member relative to the manifest and combines
all cases with the same 17-category taxonomy. Repeated IDs must have identical
content and source; conflicting labels, different category definitions, or
missing members fail validation. Private members are never silently omitted.
`-dataset` overrides `-suite` when you want a single dataset instead.

The private personal file is `evals/local/dataset.personal-labeled.json`. Its
300 AI-reviewed labels have a rationale, confidence and merchant research links
in `evals/local/personal-label-audit.json`; policy notes are in
`evals/local/review/README.md`. There are 217 high-, 63 medium-, and 20
low-confidence rows. These are reference judgments, not receipt-verified truth.
The original blank workbook remains an earlier artifact, not the current label
source. `evals/local/dataset.combined.json` is the bundled snapshot.

The public and authored sets retain their existing reference labels. See
[DATASETS.md](DATASETS.md) for licensing, coverage, source mapping and importer
reproduction. On a checkout without the gitignored personal file, use
`-suite evals/suite.public.json` for the 470 available public/authored examples.
The private file must be supplied to reproduce the full 770-case suite.

Examples contain `id`, `description`, signed `amount`, `expected`, and optional
`source` / `confidence` (`high`, `medium`, `low`). Only description, amount and
category definitions reach the model. IDs, expected labels, confidence and
research are unavailable to prompt templates. Amounts in the personal set are
already perturbed; the public corpus uses a debit-direction placeholder of -1.

The headline score weights every example equally: synthetic 170/770, public
300/770 and personal 300/770. It is not a balanced estimate of your production
traffic. Inspect source and confidence slices alongside the headline. Repeated
merchants, synthetic source data, and AI-authored labels can bias comparisons.
Hold out fresh data when tuning prompts; this benchmark is not an independent
human-annotated holdout. The runner uses one expected category per example.

## Speed and experiment controls

- `concurrency`: **8 by default**, shared across the whole matrix; configure
  1–1024 or override with `-concurrency`. Zero/omitted uses 8. Requests run in
  parallel across batches and can overlap variants. Idle connections are reused.
- `request_interval`: **`250ms` in supplied configs**, limiting starts to four
  per second across initial calls and retries. `0s` disables normal spacing,
  while retry cooldowns still apply.
- `retry`: `max_attempts: 5` (initial call plus four retries),
  `initial_backoff: "2s"`, `max_backoff: "30s"`. Delays double with each failure
  and use jitter between half and all of the backoff. `Retry-After` can extend
  the delay beyond 30s. Set `max_attempts: 1` to disable retries.
- `timeout`: per-attempt deadline, **120s** in supplied configs. Both eval
  adapters honor this deadline; the production Gemini client remains at 60s.
- `models`: unique display names, provider/model IDs and key variable names.
- `prompts`: unique names with optional `template_file` or inline `template`.
  Omit both for the production prompt. Paths are relative to the config.
  Go templates must render `{{.Categories}}` and `{{.Transactions}}`.
- `batch_sizes`: distinct integers 1–100, including any partial last batch.
  These are synchronous inference requests, not asynchronous provider Batch APIs.
- `repeats`: positive count; increases the number of requests and predictions.
- `seed`: identical shuffled order for every variant using `seed + repeat`.
  Input order is reproducible; model sampling is not seeded. Reports restore
  input order even when requests finish out of order.

HTTP 429, HTTP 408, server errors (5xx), network failures, attempt timeouts and
broken API-response envelopes are retried with exponential backoff. HTTP 200
responses containing an upstream error code follow the same policy. A 429
extends a shared cooldown, pausing fresh batches as well as retries. Cancellation
interrupts pacing and backoff immediately. Each attempt gets its own timeout.

Permanent API rejections (e.g. authentication/credit errors) are not retried.
They and exhausted transport failures are excluded from classification scores
and recorded separately. Model mistakes—unknown categories, malformed category
JSON, refusals/truncation, or wrong label counts—are scored as errors and are
not retried. This avoids giving a model extra chances to correct its output.

At 250ms spacing, the full 1,696-call matrix takes at least about seven minutes,
plus final response latency, retries and any shared cooldowns. Retries add API
calls and can add cost, even when a response is lost. The dry-run request count
is the number of initial calls, not a guarantee about total attempts.
Validation and credentials are checked before paid requests. A dry run cannot
validate remote model availability or credentials.

## Scores and outputs

Each finished variant prints and checkpoints:

- `summary.csv`: one row per variant with overall accuracy, macro F1, error
  rate, scored/excluded counts, planned coverage, retry counts, request latency,
  reported token counts/cost in USD, and usage-reporting attempt counts.
- `slices.csv`: source and confidence scores, with support counts, so the
  personal high-confidence slice can be examined separately. `unspecified`
  confidence includes public/authored data without confidence annotations.
- `excluded.csv`: each example omitted from classification scores, with its
  ID, variant, batch, provenance and failure reason.
- `request_errors.csv`: failed attempts, including recovered failures, with
  HTTP status, retryability and the final batch exclusion/exhaustion state.
- `results.json`: resolved dataset/config, dataset digest, all predictions,
  per-category metrics, confusion matrices, source/confidence slices, batch
  timing, every attempt (including usage and response ID/provider/model), separate
  `request_stats`, per-variant and whole-run `usage`, and wall time/throughput.

Accuracy is correct predictions / **scored examples**, excluding infrastructure
failures. Macro F1, per-category precision/recall and confusion counts use that
same filtered set; so do source and confidence slices. Matching ignores category
casing and outer whitespace. Unknown categories and invalid model output remain
incorrect; no General fallback is applied. Wrong but valid labels reduce accuracy
without increasing output-error rate. Confusion rows are expected categories,
columns predictions; the empty column marks invalid model output.

`attempted = evaluated + excluded`. Summary coverage is evaluated / planned;
metrics and slice coverage are evaluated / attempted (untouched cases in an
interrupted run are not attempted). With zero scored examples, CSV score cells
are blank and the CLI shows `n/a`. JSON includes `score_available: false`; zero
numeric placeholders must not be interpreted as scores. `complete` means all
batch jobs finished processing, not that coverage reached 100%.

Operational counters remain separate from classification outcome rates:
`requests` counts logical batches, `failed_requests` counts their final failures,
and `request_stats` counts actual attempts, retries, transient failures, recovered
batches, excluded batches and exhausted retries. A successfully retried batch
contributes each example exactly once. Slice metrics describe classification;
request timings belong to full variants because batches mix sources/confidence.
Report schema version 4 adds usage accounting to the version 3 exclusion rules; older saved runs retain
their original scoring and have not been rewritten.

Request latency includes network and parsing time for every attempt, excluding
backoff/pacing; batch `elapsed_ms` includes waits. Sum of request time divided
by attempted transactions is **service time, not elapsed time** under concurrency. Use
whole-run `wall_seconds` and `transactions_per_second` for scored-example throughput;
concurrent variants share resources, so latency is not an isolated speed test.
Suite hashes cover the canonical merged dataset; single-file hashes cover its file bytes.

### Token usage and cost

OpenRouter usage is recorded automatically; no extra configuration or API calls
are needed. Each completed variant prints input, output and total tokens plus
reported cost in USD. A final `Run total` line sums all processed variants,
including partial variants after Ctrl-C. JSON stores per-attempt data and variant/run
totals; `summary.csv` stores variant totals. Costs come directly from OpenRouter's
`usage.cost` (the amount charged to the account), not a model-price estimate or
`cost_details.upstream_inference_cost`. See [OpenRouter usage accounting](https://openrouter.ai/docs/cookbook/administration/usage-accounting).

Every attempt contributes its reported usage, including retries, invalid model
outputs and excluded batches. Usage accounting is independent of classification
scoring. Output tokens already include reasoning tokens; cached input tokens
are part of input tokens. Reasoning, cache-read and cache-write counts are stored
as optional breakdowns and are never added again to total tokens or cost.

When a timeout, broken response, or provider omits usage, it remains unknown.
The CLI marks totals as `incomplete` or `unavailable` and prints how many attempts
reported tokens and cost. JSON includes `attempts`, `token_reported_attempts`
and `cost_reported_attempts`; CSV includes the latter two alongside `attempts`.
A sum of reported values can understate actual billing when coverage is incomplete.
Unavailable values are JSON `null` / blank CSV cells / CLI `n/a`, distinct from
an explicitly reported zero. Optional reasoning/cache totals sum only reported
breakdowns; absence does not imply zero. Providers without usage support likewise
show unavailable accounting. Earlier runs cannot be backfilled from their saved
reports because neither response IDs nor usage were retained.

Outputs contain the dataset and remain gitignored under `evals/runs/`. Private
inputs/notes remain gitignored under `evals/local/`. New output directories use
0700 and files 0600. `-out` must name a new directory to preserve earlier runs.

Ctrl-C cancels queued and in-flight work, drains attempted results, saves partial
variants as incomplete, and exits nonzero. Untouched cases are excluded from
partial denominators. Checkpoint failures cancel remaining work and return an
error. Runs do not resume automatically. Compare only complete variants on the
same suite and scoring version. A successful process exit can still contain
excluded cases, so always compare coverage and exclusions as well as scores.
Unequal exclusions can bias comparisons even when accuracy looks high. Variants are listed in completion order.

## Development

Tests use fake providers/transports and make no API calls:

```sh
go test ./...
go test -race ./internal/eval ./internal/ai
go vet ./...
```

## DeepSeek V4.1 Flash with required ZDR

```sh
go run ./cmd/eval -config evals/config.deepseek-zdr.json
```

This separate config uses `deepseek/deepseek-v4.1-flash` (the latest alias,
resolving to the September 10, 2026 release when checked) with the same
770-case/six-variant matrix. The model's `routing` configuration is sent as
OpenRouter's `provider` object on every attempt:

```json
{"zdr": true, "data_collection": "deny", "only": ["fireworks"]}
```

The adapter does not relax these constraints on failure. There is no fallback
to another provider or model. Routing fields are rejected for native Gemini
rather than silently ignored. OpenRouter lists Fireworks as a ZDR endpoint for
this model; the public discovery snapshot is in
`sources/deepseek-v4.1-flash-zdr.json`. ZDR is an endpoint routing constraint,
not a model-name suffix. See [OpenRouter ZDR documentation](https://openrouter.ai/docs/guides/features/zdr).

Generation/reasoning settings use the provider's defaults, as in the earlier
Gemini run. DeepSeek uses reasoning by default, so comparing cost requires
including reasoning tokens. The six synthetic pilot calls all routed to
Fireworks and returned the expected label count. Their usage and request IDs
are saved locally in `evals/local/deepseek-zdr-pilot.json`.

At the observed $0.22/million input and $0.66/million output prices, pilot-based
extrapolation estimates roughly **$0.21** for the full matrix, including
reasoning, before extra retry attempts and without assuming cache discounts.
Allow approximately **$0.20–$0.40** for variation; this is a small-sample estimate,
not a billing cap. Provider/account availability and pricing can change.

## GLM 5.3 Flash with required ZDR

```sh
go run ./cmd/eval -config evals/config.glm-zdr.json
```

This focused comparison uses `z-ai/glm-5.3-flash`, the production prompt and
batch size 15 on the same 770-example suite (52 initial requests). It preserves
the DeepSeek run's seed, concurrency, pacing, timeout and retry policy, with
provider-default generation settings. Every request requires ZDR, denies data
collection and restricts routing to Fireworks, as above. The verified endpoint
snapshot is in `sources/glm-5.3-flash-zdr.json`. Token counts and reported cost
are printed and saved automatically; no production environment is needed.
