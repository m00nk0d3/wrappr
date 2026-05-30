-- name: CreateCompany :one
INSERT INTO companies (name, slug)
VALUES ($1, $2)
RETURNING *;

-- name: GetCompanyByID :one
SELECT * FROM companies WHERE id = $1;

-- name: UpdateCompanySubscriptionStatus :one
UPDATE companies
SET subscription_status = $2,
    updated_at = NOW()
WHERE id = $1
RETURNING *;
