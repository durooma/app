-- Preferences are keyed by user. The current single-user app uses "local";
-- this does not introduce authentication or separate users' transaction data.
CREATE TABLE user_settings (
    user_id TEXT PRIMARY KEY,
    background_categorization BOOLEAN NOT NULL DEFAULT false
);
