ALTER TABLE user_settings
 ADD COLUMN openrouter_key BYTEA,
 ADD COLUMN ai_model TEXT NOT NULL DEFAULT 'deepseek/deepseek-v4.1-flash',
 ADD COLUMN ai_interval_ms INTEGER NOT NULL DEFAULT 250 CHECK (ai_interval_ms BETWEEN 10 AND 86400000),
 ADD COLUMN ai_concurrency INTEGER NOT NULL DEFAULT 8 CHECK (ai_concurrency BETWEEN 1 AND 32),
 ADD COLUMN ai_batch_size INTEGER NOT NULL DEFAULT 15 CHECK (ai_batch_size BETWEEN 1 AND 100),
 ADD COLUMN ai_revision BIGINT NOT NULL DEFAULT 0;

-- The shared environment key is never migrated or used as a fallback.
UPDATE user_settings SET background_categorization = false;

CREATE TABLE user_ai_request_budget (
 user_id TEXT PRIMARY KEY REFERENCES user_settings(user_id) ON DELETE CASCADE,
 next_request_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 failures INTEGER NOT NULL DEFAULT 0
);
