-- name: GetCompanyByID :one
SELECT * FROM companies WHERE id = $1;

-- name: UpdateCompanyTier :one
UPDATE companies
SET subscription_status = $2,
    updated_at = NOW()
WHERE id = $1
RETURNING *;
