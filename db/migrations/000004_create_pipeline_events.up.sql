CREATE TABLE pipeline_events (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    job_id      UUID NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
    actor_id    UUID REFERENCES users(id) ON DELETE SET NULL, -- NULL when triggered by the system/worker
    from_status TEXT,
    to_status   TEXT NOT NULL,
    note        TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_pipeline_events_job_id     ON pipeline_events(job_id);
CREATE INDEX idx_pipeline_events_created_at ON pipeline_events(created_at DESC);
CREATE INDEX idx_pipeline_events_to_status  ON pipeline_events(to_status);
