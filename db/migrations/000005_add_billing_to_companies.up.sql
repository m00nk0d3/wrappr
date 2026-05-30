ALTER TABLE companies
    ADD COLUMN stripe_customer_id      TEXT,
    ADD COLUMN stripe_subscription_id  TEXT,
    ADD COLUMN subscription_status     TEXT NOT NULL DEFAULT 'trial',
    ADD COLUMN trial_ends_at           TIMESTAMPTZ,
    ADD COLUMN billing_email           TEXT;

CREATE INDEX idx_companies_stripe_customer_id     ON companies(stripe_customer_id);
CREATE INDEX idx_companies_stripe_subscription_id ON companies(stripe_subscription_id);
CREATE INDEX idx_companies_subscription_status    ON companies(subscription_status);
