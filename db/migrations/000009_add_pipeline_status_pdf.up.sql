-- Expand pipeline_status CHECK to include stages added by the PDF generation step.
-- 'extracted'     — LLM extraction complete; PDF generation task enqueued.
-- 'generating_pdf' — Gotenberg PDF render in progress.
ALTER TABLE jobs DROP CONSTRAINT IF EXISTS jobs_pipeline_status_check;

ALTER TABLE jobs
    ADD CONSTRAINT jobs_pipeline_status_check
    CHECK (pipeline_status IN (
        'queued',
        'processing',
        'transcribing',
        'transcribed',
        'extracting',
        'extracted',
        'generating_pdf',
        'completed',
        'failed',
        'cancelled'
    ));
