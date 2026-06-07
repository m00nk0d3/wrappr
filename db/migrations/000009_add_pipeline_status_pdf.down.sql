-- Revert pipeline_status CHECK to the state before migration 000009.
ALTER TABLE jobs DROP CONSTRAINT IF EXISTS jobs_pipeline_status_check;

ALTER TABLE jobs
    ADD CONSTRAINT jobs_pipeline_status_check
    CHECK (pipeline_status IN (
        'queued',
        'processing',
        'transcribing',
        'transcribed',
        'extracting',
        'completed',
        'failed',
        'cancelled'
    ));
