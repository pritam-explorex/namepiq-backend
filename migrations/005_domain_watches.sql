CREATE TABLE IF NOT EXISTS domain_watches (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id             UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    domain              TEXT NOT NULL,
    tier                TEXT NOT NULL DEFAULT 'free'
                        CHECK (tier IN ('free', 'standard', 'priority')),
    status              TEXT NOT NULL DEFAULT 'taken'
                        CHECK (status IN ('pending_payment', 'taken', 'available', 'abandoned')),
    last_checked_at     TIMESTAMPTZ,
    next_check_at       TIMESTAMPTZ,
    notified_at         TIMESTAMPTZ,
    razorpay_order_id   TEXT,
    razorpay_payment_id TEXT,
    price_minor         BIGINT,
    currency            TEXT,
    last_error          TEXT,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- One active watch per user+domain (abandoned rows do not block re-watch)
CREATE UNIQUE INDEX IF NOT EXISTS domain_watches_user_domain_active_uidx
    ON domain_watches (user_id, domain)
    WHERE status <> 'abandoned';

CREATE INDEX IF NOT EXISTS domain_watches_due_idx
    ON domain_watches (next_check_at)
    WHERE status = 'taken';

CREATE INDEX IF NOT EXISTS domain_watches_domain_active_idx
    ON domain_watches (domain)
    WHERE status IN ('taken', 'available');

CREATE UNIQUE INDEX IF NOT EXISTS domain_watches_razorpay_order_id_uidx
    ON domain_watches (razorpay_order_id)
    WHERE razorpay_order_id IS NOT NULL;
