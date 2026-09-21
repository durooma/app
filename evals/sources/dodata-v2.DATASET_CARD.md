---
license: mit
tags:
  - text-classification
  - finance
  - transactions
  - english
  - synthetic
task_categories:
  - text-classification
language:
  - en
size_categories:
  - 10K<n<100K
---

# US Bank Transaction Categories v2 — Synthetic Dataset

68,000 sign-prefixed transaction descriptions across 17 spending categories, modeled after real US bank statement formats. Designed for training classifiers that work on actual bank data — not the clean `"Starbucks coffee"` descriptions that most datasets use.

**Successor to [v1](https://huggingface.co/datasets/DoDataThings/us-bank-transaction-categories).**

## Why This Dataset

Real bank transaction data is private. But the _formats_ are universal — Chase, Apple Card, PayPal, Capital One, Mercury all produce descriptions with predictable structures. This dataset captures those structures with 500+ real merchant names, randomized store numbers, addresses, and reference codes.

### Why Sign Prefixes

Bank transaction descriptions are ambiguous without direction. "VENMO CASHOUT" as a credit is income — as a debit it's a transfer. `[debit]`/`[credit]` encodes the cardholder's perspective so the model can disambiguate.

Sign distributions per category reflect real-world patterns:
- Spending categories: 95%+ debit
- Income: 97% credit
- Transfer: 50/50 (bidirectional by nature)

## Format

CSV with two columns:

```
description,category
"[debit] AMAZON MKTPL*K8R2M5VN7","Shopping"
"[credit] ACME CORP       PAYROLL                    PPD ID: 123456789","Income"
"[debit] PreApproved Payment Bill User Payment: Netflix","Subscription"
"[debit] SCHWAB BROKERAGE MONEYLINK PPD ID: 5551234567","Transfer"
"[credit] AUTOMATIC PAYMENT - THANK","Transfer"
"[debit] PP*SAFEWAY","Groceries"
"[debit] TST*GOLDEN DRAGON - OAKLAND","Restaurants"
```

## Categories (17)

| Category | Count | Examples |
|----------|-------|----------|
| Restaurants | 4,000 | `TST*SAKURA SUSHI`, `DAVESHOTCHICKEN`, `CLV*BURGER JOINT`, `PP*CHIPOTLE` |
| Groceries | 4,000 | `SAFEWAY #1197`, `WHOLEFDS FRE #10467`, `PYPL*TRADER JOE'S`, `365 MARKET` |
| Shopping | 4,000 | `Amazon.com*K8R2M5VN7`, `TIKTOK SHOP`, `Express Checkout Payment: TARGET`, `TOTAL WINE` |
| Transportation | 4,000 | `CHEVRON 0385291`, `CHARGEPOINT *STATION 1234`, `COSTCO GAS #1061`, `CA DMV FEE` |
| Entertainment | 4,000 | `VALVE STEAM PURCHASE`, `DRAFTKINGS SPORTSBOOK`, `PreApproved Payment: Valve Corp.` |
| Utilities | 4,000 | `PG&E`, `PGANDE WEB ONLINE`, `REPUBLIC SERVICES TRASH`, `SUNRUN SOLAR` |
| Subscription | 4,000 | `CURSOR USAGE`, `X CORP. PAID FEATURES`, `PYPL*NETFLIX`, `SALESFORCE; Billing` |
| Healthcare | 4,000 | `TELADOC TELEHEALTH`, `KAISER PERMANENTE`, `PP*ASPEN DENTAL`, `BETTERHELP` |
| Insurance | 4,000 | `FARMERS INS BILLING`, `HOMESERVE USA`, `PreApproved Payment: STATE FARM` |
| Mortgage | 4,000 | `ROCKET MORTGAGE`, `PATELCO CU MORTGAGE`, `SOFI MORTGAGE PAYMENT`, `Principal Pmt` |
| Rent | 4,000 | `EQUITY RESIDENTIAL RENT PAYMENT`, `GREYSTAR LEASE PAYMENT` |
| Travel | 4,000 | `ROYAL CARIBBEAN`, `TSA PRECHECK`, `SWA INFLIGHT WIFI`, `MARRIOTT` |
| Education | 4,000 | `COURSERA`, `Express Checkout Payment: Scholastic Inc`, `BRILLIANT.ORG` |
| Personal Care | 4,000 | `SPORT CLIPS`, `SEPHORA`, `360 FITNESS LLC`, `TST*ISLAND VINTAGE SHAVE` |
| Transfer | 4,000 | `COINBASE ACH TRANSFER`, `WIRE TRANSFER TO NAME`, `ATM DEPOSIT`, `DDA TO DDA` |
| Income | 4,000 | `PAYROLL PPD ID:`, `SSA TREAS 310 FED SAL`, `DOORDASH DASHERPAY` |
| Fees | 4,000 | `OVERDRAFT FEE`, `ATM SURCHARGE`, `PAPER STATEMENT FEE` |

## Eight Bank Statement Formats

Every spending category produces descriptions in all major US bank formats:

| Format | Structure | Banks |
|---|---|---|
| **Chase ACH** | `INSTITUTION     PURPOSE        PPD/WEB ID: CODE` | Chase checking |
| **Chase merchant** | `MERCHANT #STORE` or `MERCHANT*ORDERID` | Chase credit cards |
| **Apple Card** | `MERCHANT ADDRESS CITY ZIP STATE COUNTRY` | Apple Card |
| **PayPal** | `PreApproved Payment: MERCHANT`, `PP*`, `PYPL*`, `PAYPAL *`, `INST XFER` | PayPal (as card issuer) |
| **Capital One** | `Withdrawal from MERCHANT`, `Preauthorized Deposit from MERCHANT` | Capital One |
| **Mercury** | `MERCHANT; Description` or just `MERCHANT` | Mercury, neobanks |
| **POS** | `SQ *MERCHANT`, `TST*MERCHANT`, `CLV*MERCHANT` | Square, Toast, Clover |
| **Simple** | `MERCHANT`, `MERCHANT.COM`, `MERCHANT INC.` | Various |

### PayPal as a Bank Format

PayPal isn't just a payment wrapper — it's a card issuer. People use PayPal credit/debit cards at any merchant. This dataset treats PayPal formats as first-class bank statement structures:

- `PreApproved Payment Bill User Payment: STARBUCKS` → Restaurants
- `PP*SAFEWAY` → Groceries
- `PYPL*NETFLIX` → Subscription
- `Express Checkout Payment: TARGET` → Shopping
- `PAYPAL INST XFER MEDIUM.COM WEB ID: PAYPALSI77` → Subscription

PayPal-formatted descriptions appear in all spending categories at realistic rates (5-15% of samples).

## Variation Dimensions

- **Capitalization:** ALL CAPS, Title Case, lowercase, mixed
- **Spacing:** normal, extra-padded (ACH style), compressed
- **Store numbers:** `#1234`, `1234`, `#01234`, absent
- **Order IDs:** `*ORDERID` (Amazon style)
- **POS prefixes:** `SQ *`, `TST*`, `CLV*`
- **PayPal prefixes:** `PP*`, `PYPL*`, `PAYPAL *`, `PreApproved Payment`, `Express Checkout`
- **Addresses:** full, partial, zip-smashed-into-city (Apple Card quirk)
- **Compressed names:** `DAVESHOTCHICKEN`, `CHICKFILA`, `WHOLEFDS FRE`, `TRADERJOES`
- **Cities:** 36 US cities across multiple states
- **Sign prefixes:** `[debit]`/`[credit]` with category-appropriate distributions

## Design Decisions

- **No "Housing" category.** Split into Mortgage (model-classified) and Rent (model-classified). Home maintenance → Shopping.
- **No "Business" category.** Whether a transaction is a business expense depends on the _account_, not the description.
- **Transfer vs Income uses sign.** `[credit] VENMO CASHOUT` = Income. `[debit] VENMO PAYMENT TO` = Transfer. The sign prefix is the primary disambiguator.
- **Subscription vs Shopping uses format cues.** `Amazon.com*ORDERID` = Shopping. `AMAZON WEB SERVICES` = Subscription. `X CORP. PAID FEATURES` = Subscription. `TIKTOK SHOP` = Shopping.
- **500+ real merchants** across restaurants (including culturally diverse cuisines), groceries, SaaS/AI tools, fintech platforms, crypto brokerages, EV charging, gambling/sportsbooks, and more.
- **Balanced classes.** 4,000 samples per category prevents the classifier from defaulting to the most common class.

## Usage

```python
from datasets import load_dataset

ds = load_dataset("DoDataThings/us-bank-transaction-categories-v2")
print(ds["train"][0])
# {'description': '[debit] AMAZON MKTPL*K8R2M5VN7', 'category': 'Shopping'}
```

## Trained Model

A DistilBERT model fine-tuned on this dataset is available at [DoDataThings/distilbert-us-transaction-classifier-v2](https://huggingface.co/DoDataThings/distilbert-us-transaction-classifier-v2) — 99.9% validation accuracy, 96% of real-world classifications at 0.90+ confidence.

## Generator

The synthetic data generator is open source:

```bash
node scripts/generate-training-data.js --count 4000  # 4,000 per category
```

Available at [github.com/wnstnb/foliome](https://github.com/wnstnb/foliome).

## License

MIT
