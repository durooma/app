-- Who chose a transaction's amortization window: NULL while it still sits in
-- the month it occurred and nobody has decided, 'user' once set by hand, 'ai'
-- once suggested by the categorizer. The AI only ever fills NULL, so it never
-- overrides a hand-picked period.
ALTER TABLE transactions
    ADD COLUMN period_source TEXT CHECK (period_source IN ('user', 'ai'));

-- Any window other than the transaction's own month was set by hand before
-- this column existed.
UPDATE transactions SET period_source = 'user'
WHERE start_month <> date_trunc('month', txn_date)::date OR end_month <> start_month;

-- The background queue now holds transactions missing a category, a period, or
-- both.
DROP INDEX idx_transactions_categorize_due;
CREATE INDEX idx_transactions_classify_due
    ON transactions (categorize_after, id) WHERE category_id IS NULL OR period_source IS NULL;
