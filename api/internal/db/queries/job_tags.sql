-- name: ListJobTagsByJob :many
SELECT * FROM job_tags WHERE job_id = $1;

-- name: CreateJobTag :one
INSERT INTO job_tags (job_id, tag)
VALUES ($1, $2)
RETURNING *;

-- name: DeleteJobTag :exec
DELETE FROM job_tags WHERE job_id = $1 AND tag = $2;
