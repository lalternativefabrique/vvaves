CREATE TABLE IF NOT EXISTS transcription_job (
    id          UUID PRIMARY KEY,
    app         TEXT NOT NULL DEFAULT '',
    reference   TEXT NOT NULL,
    url         TEXT NOT NULL,
    language    TEXT NOT NULL DEFAULT '',
    status      TEXT NOT NULL,
    error       TEXT NOT NULL DEFAULT '',
    result      JSONB,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    finished_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS transcription_job_pending ON transcription_job (created_at)
    WHERE status = 'pending';
