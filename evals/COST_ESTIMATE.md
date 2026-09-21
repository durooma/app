# OpenRouter eval cost estimate

Estimated 2026-09-20 for `config.compare.json` (also the current `config.json`):
770 examples, one `google/gemini-3.1-flash-lite` model, two prompts, batch sizes
1/15/30, one repeat. Total: 1,696 initial calls and 4,620 classifications. The current retry policy
allows up to five attempts per batch; the estimates below exclude extra
attempts. Retries can increase actual usage and cost.

All requests were rendered locally using the actual Go prompt renderer,
category definitions, sanitized dataset, seeded shuffle and batch boundaries.
No inference API calls were made. Dataset SHA-256:
`2cccf7c84530fab0367b0b7aed83be822309a319ed188ae58fc9a3aafa8e6aea`.

| Quantity | Estimate |
|---|---:|
| Input tokens | ~620,000 |
| Visible output tokens | ~22,000 |
| Input cost | ~$0.155 |
| Visible output cost | ~$0.033 |
| Total without additional reasoning | **~$0.19 USD** |

[OpenRouter's listed rates](https://openrouter.ai/google/gemini-3.1-flash-lite)
are $0.25 per million input tokens and $1.50 per million output tokens.
The six variants contain 2,423,612 input characters in total. The central
estimate assumes four characters per input token plus eight tokens per call
for message wrapping. Actual Gemini tokenization is not measured. Using
three to five characters per token gives approximately 500k–822k input tokens.
Output assumes four tokens per category and two per response for JSON framing:
21,872 tokens, rounded above. Shorter/longer category names, whitespace and
invalid responses can change this. A reasonable text-token-only planning range
is **$0.15–$0.25**, not a guaranteed billing limit.

Google documents Flash-Lite's [default minimal thinking level](https://ai.google.dev/gemini-api/docs/generate-content/thinking),
which approximates no thinking for most queries. The eval leaves generation
settings at provider defaults. Additional billable reasoning tokens cannot be
predicted from the final category array: 100 extra output tokens per request
would add $0.2544; 1,000 would add $2.544. No cache discount is assumed. Credit
purchase fees, taxes and any different upstream-provider price are excluded.
New runs record OpenRouter-reported token usage and cost in the CLI and reports.
Check usage coverage: timed-out or otherwise missing responses may leave the
reported total below actual billing. The historical estimates above remain estimates.

Most input cost comes from batch size 1 repeating the full instructions and
category list for each transaction. The prompt-character totals below include
both prompt variants:

| Batch size | Requests | Input characters |
|---|---:|---:|
| 1 | 1,540 | 2,006,688 |
| 15 | 104 | 240,200 |
| 30 | 52 | 176,724 |
