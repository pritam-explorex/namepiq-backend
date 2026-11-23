ALTER TABLE users
    ADD COLUMN IF NOT EXISTS preferred_currency TEXT NOT NULL DEFAULT 'USD',
    ADD COLUMN IF NOT EXISTS country TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS users_preferred_currency_idx ON users (preferred_currency);
