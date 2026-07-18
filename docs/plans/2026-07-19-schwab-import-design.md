# Schwab CSV Import Design

## Context

Durooma's Schwab parser currently recognizes the common `Date`-first CSV header,
but only emits transactions for the extended Equity Awards actions. Schwab's
Individual and Joint Tenant exports use a compact eight-column layout and contain
ordinary signed cash transactions such as interest, card purchases, dividends,
tax adjustments, and wires. Those rows currently produce no output.

## Design

The parser will identify Equity Awards exports by their extended lot-accounting
columns, especially `VestFairMarketValue`. That layout keeps the existing
specialized behavior: an RS deposit creates grant income equal to vested shares
times vest fair-market value, and a sale creates a separate signed gain/loss equal
to the sum of each lot's `(sale price - vest fair-market value) * shares`, less
fees. A negative result is therefore an expense and a positive result is income.

The compact brokerage layout will import every dated row whose `Amount` cell is
nonblank, preserving Schwab's signed amount. Its export does not include cost
basis, so a `Sell` row paired with an immediately preceding matching `Stock Plan
Activity` row will be skipped rather than treating full proceeds as income; the
Equity Awards export is authoritative for that gain or loss. Blank-amount
informational rows will also be skipped. Descriptions will combine action,
symbol, and Schwab description so transactions remain readable and stable for
deduplication.

When no account name is supplied, extended exports use `Schwab Equity` and
compact exports use `Schwab Brokerage`. Users can continue supplying explicit
names such as `Schwab Individual` and `Schwab Joint Tenant` to keep accounts
separate.

Transaction hashes remain stable, but uniqueness is scoped to the destination
account. Re-importing a row into one account is still deduplicated while an
identical legitimate row in another account is retained.

## Error handling and verification

Both formats require a `Date` header. Compact rows with invalid dates or blank
amounts are ignored consistently with existing parsers. Tests will cover compact
positive and negative amounts, blank informational rows, account naming, and
both positive and negative Equity Awards sale differences. The full Go test suite
will guard the importer and surrounding application behavior.
