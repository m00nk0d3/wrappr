-- name: CreatePipelineEvent :one
INSERT INTO pipeline_events (job_id, actor_id, from_status, to_status, note)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: ListPipelineEventsByJob :many
SELECT * FROM pipeline_events WHERE job_id = $1 ORDER BY created_at ASC;
