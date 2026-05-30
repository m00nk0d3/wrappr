-- name: ListJobMaterialsByJob :many
SELECT * FROM job_materials WHERE job_id = $1;

-- name: CreateJobMaterial :one
INSERT INTO job_materials (job_id, name, quantity, unit)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: DeleteJobMaterial :exec
DELETE FROM job_materials WHERE id = $1;
