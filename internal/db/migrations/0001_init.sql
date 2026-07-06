-- Core schema for the personal finance app.

CREATE TABLE institutions (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name       TEXT NOT NULL UNIQUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE accounts (
    id             BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    institution_id BIGINT NOT NULL REFERENCES institutions(id) ON DELETE CASCADE,
    name           TEXT NOT NULL,
    currency       TEXT NOT NULL,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (institution_id, name)
);

CREATE TABLE categories (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name        TEXT NOT NULL UNIQUE,
    description TEXT NOT NULL DEFAULT '',
    -- Optional hint: 'income', 'expense', or NULL (inferred from amount sign).
    kind        TEXT CHECK (kind IN ('income', 'expense')),
    sort_order  INT NOT NULL DEFAULT 0,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE transactions (
    id            BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    account_id    BIGINT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    txn_date      DATE NOT NULL,
    description   TEXT NOT NULL,
    amount        NUMERIC(20, 4) NOT NULL,           -- original amount
    currency      TEXT NOT NULL,                     -- original currency
    base_amount   NUMERIC(20, 4) NOT NULL,           -- converted to base currency
    base_currency TEXT NOT NULL,
    category_id   BIGINT REFERENCES categories(id) ON DELETE SET NULL,
    -- Amortization window: by default both equal the first-of-month of txn_date,
    -- but a transaction can be spread across a range (quarter, year, ...).
    start_month   DATE NOT NULL,
    end_month     DATE NOT NULL,
    external_hash TEXT NOT NULL UNIQUE,              -- dedup key (ports generateID)
    source        TEXT NOT NULL DEFAULT '',          -- import provider label
    note          TEXT NOT NULL DEFAULT '',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (end_month >= start_month)
);

CREATE INDEX idx_transactions_date ON transactions (txn_date);
CREATE INDEX idx_transactions_account ON transactions (account_id);
CREATE INDEX idx_transactions_category ON transactions (category_id);
CREATE INDEX idx_transactions_months ON transactions (start_month, end_month);

-- Substring rules applied before falling back to the AI categorizer.
CREATE TABLE rules (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    pattern     TEXT NOT NULL,
    category_id BIGINT NOT NULL REFERENCES categories(id) ON DELETE CASCADE,
    priority    INT NOT NULL DEFAULT 0,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Cached FX rates (base -> quote on a given date), auto-filled from the FX API.
CREATE TABLE exchange_rates (
    rate_date DATE NOT NULL,
    base      TEXT NOT NULL,
    quote     TEXT NOT NULL,
    rate      NUMERIC(20, 8) NOT NULL,
    PRIMARY KEY (rate_date, base, quote)
);

CREATE TABLE settings (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
