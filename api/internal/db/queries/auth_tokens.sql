-- name: CreateAuthToken :one
INSERT INTO auth_tokens (user_id, token_hash, expires_at)
VALUES ($1, $2, $3)
RETURNING *;

-- name: GetValidAuthTokenByHash :one
SELECT * FROM auth_tokens
WHERE token_hash = $1
  AND used_at IS NULL
  AND expires_at > NOW();

-- name: UseAuthToken :one
UPDATE auth_tokens
SET used_at = NOW()
WHERE token_hash = $1
  AND used_at IS NULL
  AND expires_at > NOW()
RETURNING *;
