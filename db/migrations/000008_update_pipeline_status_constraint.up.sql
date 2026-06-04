-- Expand pipeline_status to cover every stage of the extraction pipeline.
-- The original constraint from migration 002 only included the broad states
-- ('queued', 'processing', 'completed', 'failed', 'cancelled'). The worker
-- now advances through fine-grained stages so each step is observable.
--
-- We keep 'processing' and 'cancelled' for backward compatibility in case any
-- existing rows or external code references them.
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
