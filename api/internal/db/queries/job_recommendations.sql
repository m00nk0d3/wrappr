-- name: ListJobRecommendationsByJob :many
SELECT * FROM job_recommendations WHERE job_id = $1;

-- name: CreateJobRecommendation :one
INSERT INTO job_recommendations (job_id, description, urgency, estimated_cost_range)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: ResolveJobRecommendation :one
UPDATE job_recommendations
SET resolved = true,
    resolved_job_id = $2
WHERE id = $1
RETURNING *;
