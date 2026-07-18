# Schwab CSV Import Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Import Schwab Individual and Joint Tenant CSV exports while preserving correct Equity Awards grant and sale gain/loss accounting.

**Architecture:** Detect the extended Equity Awards schema from normalized headers and route rows through specialized equity logic or compact brokerage logic. Skip compact stock-plan proceeds that lack cost basis, and scope deduplication to each account. Keep the provider-facing `ParseSchwab` API unchanged.

**Tech Stack:** Go, `encoding/csv`, table-driven unit tests.

---

### Task 1: Add compact brokerage parser coverage

**Files:**
- Modify: `internal/importer/parsers_test.go`

**Step 1:** Add a test containing the compact `Fees & Comm` schema with interest,
card purchase, cash dividend, tax adjustment, and blank-amount stock-plan rows.

**Step 2:** Run `go test ./internal/importer -run TestParseSchwabBrokerage -v` and
verify it fails because the current parser emits no compact transactions.

### Task 2: Add negative Equity Awards sale coverage

**Files:**
- Modify: `internal/importer/parsers_test.go`

**Step 1:** Add a sale lot whose sale price is below vest fair-market value and
assert that the emitted sale amount is negative after fees.

**Step 2:** Run `go test ./internal/importer -run TestParseSchwabEquitySaleLoss -v`
and verify the test fails before implementation if the required behavior is absent.

### Task 3: Implement format-aware parsing

**Files:**
- Modify: `internal/importer/schwab.go`

**Step 1:** Extract header discovery and format detection without changing the
public parser signature.

**Step 2:** Preserve the Equity Awards grant, sale, dividend, and tax behavior.

**Step 3:** Add compact brokerage parsing for dated, nonblank-amount rows,
including readable action/symbol/description text and a brokerage account default.
Skip a `Sell` paired with a matching `Stock Plan Activity` row because only the
Equity Awards schema has the basis needed for gain/loss accounting.

**Step 4:** Run the focused parser tests and keep implementation minimal until
they pass.

### Task 4: Verify the complete change

**Files:**
- Modify if needed: `README.md`
- Create: `internal/db/migrations/0003_account_scoped_transaction_dedup.sql`
- Modify: `internal/store/transactions.go`
- Test: `internal/store/integration_test.go`

**Step 1:** Change transaction uniqueness and insert conflict handling from a
global external hash to `(account_id, external_hash)`, and test that duplicates
within one account are skipped while identical rows in another account remain.

**Step 2:** Run `gofmt` on changed Go files.

**Step 3:** Run `go test ./internal/importer -v`.

**Step 4:** Run the store and importer integration tests against PostgreSQL.

**Step 5:** Run `go test ./...`.

**Step 6:** Review `git diff --check` and the final diff against all three sample
schemas.
