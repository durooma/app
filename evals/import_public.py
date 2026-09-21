#!/usr/bin/env python3
"""Build the checked-in public eval sample from a pinned, local source CSV.

Uses only Python's standard library. Does not call a model or send local data.
See DATASETS.md for the source download, license, mapping, and limitations.
"""

import argparse
import csv
import hashlib
import io
import json
from collections import Counter, defaultdict
from pathlib import Path

ROOT = Path(__file__).resolve().parent
REPO = "DoDataThings/us-bank-transaction-categories-v2"
REVISION = "3e8d052cb7b362fb1a04b6064d2331982553ea56"
SOURCE_SHA256 = "0424aed6e76f74a5b3b1ff61ccec43bc321622e6806da353b910b3b2c8108f6e"
SOURCE_URL = f"https://huggingface.co/datasets/{REPO}/resolve/{REVISION}/transactions-synthetic.csv"
MAPPING = {
    "Restaurants": "Dining",
    "Groceries": "Groceries",
    "Shopping": "Shopping",
    "Transportation": "Transport",
    "Entertainment": "Entertainment",
    "Utilities": "Utilities",
    "Healthcare": "Health",
    "Mortgage": "Housing",
    "Rent": "Housing",
    "Travel": "Travel",
    "Fees": "Fees",
}
PER_CATEGORY = 30
SEED = "durooma-public-us-v1"
AMBIGUOUS_TERMS = {
    "instacart": "Source Restaurants label can conflict with grocery-delivery interpretation.",
    "daily cash adjustment": "Description does not establish that this is a fee.",
}


def build(source):
    raw = source.read_bytes()
    if hashlib.sha256(raw).hexdigest() != SOURCE_SHA256:
        raise ValueError("source checksum mismatch; use the pinned download in DATASETS.md")
    rows = list(csv.DictReader(io.StringIO(raw.decode("utf-8"))))
    if len(rows) != 68000 or set(rows[0]) != {"description", "category"}:
        raise ValueError("unexpected source schema or row count")
    candidates = defaultdict(list)
    excluded = Counter()
    # Group identical descriptions globally so cross-category conflicts are
    # excluded as well as same-category duplicates.
    for row_number, row in enumerate(rows, 1):
        if row["category"] not in MAPPING:
            excluded["unsupported_category"] += 1
            continue
        if not row["description"].startswith("[debit] "):
            excluded["non_debit"] += 1
            continue
        desc = row["description"][len("[debit] "):].strip()
        if not desc:
            raise ValueError(f"empty description in source row {row_number}")
        normalized = " ".join(desc.casefold().split())
        if any(term in normalized for term in AMBIGUOUS_TERMS):
            excluded["ambiguous_description"] += 1
            continue
        candidates[normalized].append((row_number, row["category"], desc))

    pools = defaultdict(list)
    for normalized, matches in candidates.items():
        if len({MAPPING[match[1]] for match in matches}) != 1:
            excluded["conflicting_labels"] += len(matches)
            continue
        excluded["duplicate_description"] += len(matches) - 1
        row_number, source_category, desc = matches[0]
        rank = hashlib.sha256((SEED + "\n" + normalized).encode()).hexdigest()
        pools[MAPPING[source_category]].append((rank, row_number, source_category, desc))

    examples, provenance = [], []
    for category in sorted(set(MAPPING.values())):
        pool = sorted(pools[category])
        if len(pool) < PER_CATEGORY:
            raise ValueError(f"insufficient unique examples for {category}")
        for _, row_number, source_category, desc in pool[:PER_CATEGORY]:
            example_id = f"dodata-v2-row-{row_number:05d}"
            examples.append({
                "id": example_id,
                "description": desc,
                # The corpus supplies direction but no monetary amount. Use
                # one fixed magnitude, never category-dependent fake amounts.
                "amount": -1,
                "expected": category,
            })
            provenance.append({"id": example_id, "source_row": row_number, "source_category": source_category})

    taxonomy = json.loads((ROOT / "dataset.example.json").read_text())["categories"]
    dataset = {"name": "public-us-dodata-v2-sample-v1", "categories": taxonomy, "examples": examples}
    manifest = {
        "dataset": dataset["name"],
        "source": f"https://huggingface.co/datasets/{REPO}",
        "revision": REVISION,
        "download_url": SOURCE_URL,
        "source_sha256": SOURCE_SHA256,
        "source_rows": len(rows),
        "license": "MIT (declared in upstream dataset card)",
        "source_kind": "synthetic",
        "selection_seed": SEED,
        "selection": "first 30 per mapped category sorted by SHA256(seed + newline + normalized description)",
        "mapping": MAPPING,
        "ambiguous_description_exclusions": AMBIGUOUS_TERMS,
        "excluded_rows": dict(sorted(excluded.items())),
        "eligible_unique_by_category": {key: len(pools[key]) for key in sorted(pools)},
        "amount_policy": "all -1; source debit direction retained, no real amounts available",
        "description_policy": "strip [debit] prefix and outer whitespace; otherwise preserve source text",
        "source_row_policy": "one-based data row, excluding CSV header",
        "rows": provenance,
    }
    return dataset, manifest


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("source", type=Path, help="local transactions-synthetic.csv at the pinned revision")
    parser.add_argument("--check", action="store_true", help="verify checked-in outputs instead of writing")
    args = parser.parse_args()
    dataset, manifest = build(args.source)
    outputs = {
        ROOT / "dataset.public-us.json": dataset,
        ROOT / "sources" / "dodata-v2.manifest.json": manifest,
    }
    for path, value in outputs.items():
        content = json.dumps(value, ensure_ascii=False, indent=2) + "\n"
        if args.check:
            if path.read_text() != content:
                raise SystemExit(f"out-of-date output: {path}")
        else:
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text(content)
    print(f"{'Verified' if args.check else 'Wrote'} {len(dataset['examples'])} examples across {len(set(MAPPING.values()))} categories")


if __name__ == "__main__":
    main()
