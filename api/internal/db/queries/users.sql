-- name: GetUserByEmail :one
SELECT * FROM users WHERE email = $1 LIMIT 1;

-- name: GetUserByID :one
SELECT * FROM users WHERE id = $1;

-- name: CreateUser :one
INSERT INTO users (company_id, email, name, role)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: ListUsersByCompany :many
SELECT * FROM users WHERE company_id = $1 ORDER BY created_at ASC;
