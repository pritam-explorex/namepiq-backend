ALTER TABLE domain_watches
    ADD COLUMN IF NOT EXISTS expires_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS domain_watches_expires_at_idx
    ON domain_watches (expires_at)
    WHERE status IN ('taken', 'available');
