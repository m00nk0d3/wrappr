DROP INDEX IF EXISTS idx_companies_subscription_status;
DROP INDEX IF EXISTS idx_companies_stripe_subscription_id;
DROP INDEX IF EXISTS idx_companies_stripe_customer_id;

ALTER TABLE companies
    DROP COLUMN IF EXISTS billing_email,
    DROP COLUMN IF EXISTS trial_ends_at,
    DROP COLUMN IF EXISTS subscription_status,
    DROP COLUMN IF EXISTS stripe_subscription_id,
    DROP COLUMN IF EXISTS stripe_customer_id;
