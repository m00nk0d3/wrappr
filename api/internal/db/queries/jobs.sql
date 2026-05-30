-- name: CreateJob :one
INSERT INTO jobs (
    company_id, technician_id, client_name, client_email, client_phone,
    job_address, job_lat, job_lng, job_type, technician_notes, audio_url, photo_urls,
    detected_language, report_language
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14
) RETURNING *;

-- name: GetJobByID :one
SELECT * FROM jobs WHERE id = $1;

-- name: ListJobsByCompany :many
SELECT * FROM jobs WHERE company_id = $1 ORDER BY submitted_at DESC;

-- name: ListJobsByTechnician :many
SELECT * FROM jobs WHERE technician_id = $1 ORDER BY submitted_at DESC;

-- name: UpdateJobStatus :one
UPDATE jobs
SET pipeline_status = $2, updated_at = NOW()
WHERE id = $1
RETURNING *;

-- name: UpdateJobPipeline :one
UPDATE jobs
SET pipeline_status = $2,
    transcript = $3,
    ai_summary = $4,
    ai_work_performed = $5,
    ai_follow_up_notes = $6,
    ai_warranty_notes = $7,
    ai_job_category = $8,
    ai_client_sentiment = $9,
    ai_labor_hours = $10,
    ai_follow_up_required = $11,
    ai_safety_concerns = $12,
    ai_tags = $13,
    ai_raw_json = $14,
    ai_model_used = $15,
    ai_processed_at = $16,
    pdf_url = $17,
    email_sent_at = $18,
    completed_at = $19,
    updated_at = NOW()
WHERE id = $1
RETURNING *;
