CREATE TABLE job_materials (
    id       UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    job_id   UUID NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
    name     TEXT NOT NULL,
    quantity TEXT,
    unit     TEXT
);

CREATE INDEX idx_job_materials_job_id ON job_materials(job_id);

CREATE TABLE job_recommendations (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    job_id               UUID NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
    description          TEXT NOT NULL,
    urgency              TEXT NOT NULL CHECK (urgency IN ('immediate', 'within_30_days', 'when_convenient')),
    estimated_cost_range TEXT,
    resolved             BOOLEAN NOT NULL DEFAULT FALSE,
    resolved_job_id      UUID REFERENCES jobs(id)
);

CREATE INDEX idx_job_recommendations_job_id  ON job_recommendations(job_id);
CREATE INDEX idx_job_recommendations_resolved ON job_recommendations(resolved) WHERE resolved = FALSE;

-- Relational tags allow querying "all jobs with tag X" efficiently,
-- complementing the ai_tags TEXT[] column used for GIN-based filtering.
CREATE TABLE job_tags (
    id     UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    job_id UUID NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
    tag    TEXT NOT NULL,
    UNIQUE (job_id, tag)
);

CREATE INDEX idx_job_tags_job_id ON job_tags(job_id);
CREATE INDEX idx_job_tags_tag    ON job_tags(tag);
