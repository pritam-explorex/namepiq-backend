CREATE TABLE IF NOT EXISTS domain_comments (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    domain      TEXT NOT NULL,
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    body        TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    hidden_at   TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS domain_comments_domain_created_idx
    ON domain_comments (domain, created_at DESC);
