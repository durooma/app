# Evaluation datasets

The default combined suite has 770 examples: 470 public/authored cases plus
300 AI-reviewed personal cases stored locally. Source scores preserve the
differences in coverage; these are not independently human-labeled holdouts.

| File | Examples | Coverage | Purpose |
|---|---:|---|---|
| `dataset.example.json` | 34 | 2 per category, all 17 categories | Original quick smoke test; included in the combined default |
| `dataset.expanded.json` | 170 | 10 per category, all 17 categories | Authored multilingual and edge-case regression suite |
| `dataset.public-us.json` | 300 | 30 per category, 10 mapped categories | Reproducible sample of a public synthetic corpus |

The expanded suite includes the original 34 examples, so do not combine their
scores as independent observations. It adds English, German, French, and Italian
descriptions; Swiss and other European merchants; noisy payment references;
refunds; stock-grant/sale/tax distinctions; realized losses; transfers; and two
instruction-like or JSON-like transaction descriptions.

Amounts in the authored suites are fictional base-currency amounts. Labels
follow the existing taxonomy: explicit refunds go to `Other Income`, realized
investment losses remain `Investment-Sell`, and transfers/unknown-purpose
transactions go to `General`. These are explicit test conventions, not universal
accounting rules. Review these conventions against your personal categories.

```sh
go run ./cmd/eval -dataset evals/dataset.expanded.json -dry-run
go run ./cmd/eval -dataset evals/dataset.public-us.json -dry-run

# Remove -dry-run to call the configured models, after loading the API key.
```

The default matrix makes 376 requests on the expanded suite and 660 on the
public sample when each is run alone. The current default uses 8 workers, 250ms request spacing
and retries for transient failures. No models were called when preparing these datasets.

## Public corpus selected

[DoDataThings/us-bank-transaction-categories-v2](https://huggingface.co/datasets/DoDataThings/us-bank-transaction-categories-v2)
contains 68,000 synthetic US transaction descriptions, with 17 source categories
and realistic bank-format noise. Its publisher declares MIT licensing. The
source supplies descriptions and labels, but no monetary amounts.

The checked-in subset contains debit examples from compatible categories. It
maps Restaurants → Dining, Transportation → Transport, Healthcare → Health,
and Rent/Mortgage → Housing; six other category names map directly. Income,
Insurance, Subscription, Education, Personal Care, and Transfer are excluded
because their definitions do not map cleanly to the app's categories. Credit
rows are excluded because refund conventions differ.

The sample preserves all 17 app category definitions as model choices, even
though only 10 have expected-label support. It cannot measure salary, dividend,
stock-grant, investment-sale, other-income, tax, or General recall. Compare its
macro F1 only with other runs on this same dataset.

The import removes the `[debit]` prefix and sets every amount to **-1**. This
preserves direction without inventing category-dependent amounts. The sample
therefore measures description classification, not amount-aware reasoning.

Selection is balanced after mapping, normalizes casing/whitespace for duplicate
detection, drops conflicting labels, and ranks candidates using a fixed SHA-256
seed. Each ID contains the original one-based CSV data-row number (header
excluded). Exact duplicates are removed; related merchants and formatting
variants may remain. A spot-check also identified two ambiguous description
families, Instacart and daily cash adjustments, which the importer explicitly
excludes without relabeling. This is not a claim that all remaining labels have
been independently verified.

Public synthetic data can overstate performance: templates repeat, labels may
be noisy, and models may have seen the corpus during training. Use this suite
for comparisons and regression checks, then confirm the winner on reviewed
private transactions, especially Swiss merchants and investment activity.

## Provenance and reproduction

- Publisher: DoDataThings; dataset title: US Bank Transaction Categories v2.
- Pinned revision: `3e8d052cb7b362fb1a04b6064d2331982553ea56`.
- Source SHA-256: `0424aed6e76f74a5b3b1ff61ccec43bc321622e6806da353b910b3b2c8108f6e`.
- [Original dataset card](sources/dodata-v2.DATASET_CARD.md) is preserved verbatim.
- [Manifest](sources/dodata-v2.manifest.json) records selection rules, exclusions,
  source labels, and row numbers for every imported example.
- [License notice](sources/dodata-v2.LICENSE.txt) accompanies the derived sample.

Download the pinned source to a temporary location and use the standard-library
Python importer. The importer checks the source checksum before processing it.

```sh
curl --fail --location \
  'https://huggingface.co/datasets/DoDataThings/us-bank-transaction-categories-v2/resolve/3e8d052cb7b362fb1a04b6064d2331982553ea56/transactions-synthetic.csv' \
  --output /tmp/dodata-transactions-synthetic.csv

# Verify the committed dataset and manifest byte for byte.
python3 evals/import_public.py /tmp/dodata-transactions-synthetic.csv --check

# Rebuild both files (for a deliberate update to the importer).
python3 evals/import_public.py /tmp/dodata-transactions-synthetic.csv
```

## Other public candidates

- [Mitul Shah's transaction-categorization dataset](https://huggingface.co/datasets/mitulshah/transaction-categorization)
  lists about 4.5 million records, MIT licensing, and five countries. Access is
  gated behind sharing contact information. Its broad labels merge concepts
  such as groceries and dining, making direct app-taxonomy evaluation harder.
  It is predominantly synthetic according to its dataset card. Not imported.
- [gbadedata/transaction-classification](https://github.com/gbadedata/transaction-classification)
  describes 259,000 realistic transactions, but explicitly states that the data
  provenance and data license are unverified. The repository's MIT license is
  for its code. Not imported.

Public availability alone does not make either source an independent gold
standard. The selected public sample is useful additional coverage, while the
authored suite fills taxonomy and language gaps.

## Combined default benchmark

The default `evals/suite.json` now combines these sets with 300 AI-reviewed
personal examples kept in gitignored `evals/local/`: **770 unique IDs**. The
34-case smoke set is a subset of the 170-case authored set and is deduplicated.
All members share the same category definitions. Personal labels include
confidence and a separate per-example research audit; they are not independently
human-verified. The runner reports overall, source and confidence scores.
`evals/suite.public.json` runs just the 470 public/authored cases when private
files are unavailable. See [runner documentation](README.md).
