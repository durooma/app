-- Identical transactions can legitimately occur in separate accounts. Keep
-- re-import deduplication within an account instead of applying it globally.
ALTER TABLE transactions
    DROP CONSTRAINT IF EXISTS transactions_external_hash_key;

CREATE UNIQUE INDEX transactions_account_external_hash_key
    ON transactions (account_id, external_hash);
