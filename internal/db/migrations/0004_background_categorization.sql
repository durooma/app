-- Uncategorized transactions are the durable queue, including existing imports.
ALTER TABLE transactions
    ADD COLUMN categorize_after TIMESTAMPTZ NOT NULL DEFAULT now(),
    ADD COLUMN categorize_attempts INTEGER NOT NULL DEFAULT 0;
CREATE INDEX idx_transactions_categorize_due
    ON transactions (categorize_after, id) WHERE category_id IS NULL;

-- Shared by automatic and manual requests; restarts do not reset the budget.
CREATE TABLE ai_request_budget (
    id BOOLEAN PRIMARY KEY DEFAULT true CHECK (id),
    next_request_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    failures INTEGER NOT NULL DEFAULT 0
);
INSERT INTO ai_request_budget (id) VALUES (true);
