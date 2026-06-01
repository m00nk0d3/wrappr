-- Revert to the original pipeline_status constraint from migration 002.
ALTER TABLE jobs DROP CONSTRAINT IF EXISTS jobs_pipeline_status_check;

ALTER TABLE jobs
    ADD CONSTRAINT jobs_pipeline_status_check
    CHECK (pipeline_status IN ('queued', 'processing', 'completed', 'failed', 'cancelled'));
