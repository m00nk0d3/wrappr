CREATE TABLE jobs (
    -- Core fields
    id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id            UUID NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
    technician_id         UUID NOT NULL REFERENCES users(id),
    client_name           TEXT NOT NULL,
    client_email          TEXT,
    client_phone          TEXT,
    job_address           TEXT,
    job_lat               DECIMAL(9,6),
    job_lng               DECIMAL(9,6),
    job_type              TEXT,
    technician_notes      TEXT,
    submitted_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at          TIMESTAMPTZ,
    pipeline_status       TEXT NOT NULL DEFAULT 'queued',
    audio_url             TEXT,
    photo_urls            TEXT[],
    pdf_url               TEXT,
    email_sent_at         TIMESTAMPTZ,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    -- AI-extracted fields
    transcript            TEXT,
    ai_summary            TEXT,
    ai_work_performed     TEXT,
    ai_follow_up_notes    TEXT,
    ai_warranty_notes     TEXT,
    ai_job_category       TEXT,
    ai_client_sentiment   TEXT,
    ai_labor_hours        DECIMAL(4,1),
    ai_follow_up_required BOOLEAN NOT NULL DEFAULT FALSE,
    ai_safety_concerns    TEXT[],
    ai_tags               TEXT[],
    ai_raw_json           JSONB,
    ai_model_used         TEXT,
    ai_processed_at       TIMESTAMPTZ,

    -- i18n (BCP 47 language codes, e.g. "en", "pt", "es")
    detected_language     TEXT,
    report_language       TEXT
);

-- Core query indexes
CREATE INDEX idx_jobs_company_id       ON jobs(company_id);
CREATE INDEX idx_jobs_technician_id    ON jobs(technician_id);
CREATE INDEX idx_jobs_submitted_at     ON jobs(submitted_at DESC);
CREATE INDEX idx_jobs_pipeline_status  ON jobs(pipeline_status);
CREATE INDEX idx_jobs_ai_job_category  ON jobs(ai_job_category);
CREATE INDEX idx_jobs_client_email     ON jobs(company_id, client_email);

-- GIN index for fast tag-based filtering
CREATE INDEX idx_jobs_ai_tags ON jobs USING GIN(ai_tags);

-- Full-text search across client name, AI summary, and job address
CREATE INDEX idx_jobs_search ON jobs USING GIN(
    to_tsvector('english',
        coalesce(client_name, '') || ' ' ||
        coalesce(ai_summary, '')  || ' ' ||
        coalesce(job_address, '')
    )
);
