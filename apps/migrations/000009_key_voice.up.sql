DROP TABLE IF EXISTS default_voice;

CREATE TABLE IF NOT EXISTS key_voice (
    key_id     TEXT PRIMARY KEY,
    owner_id   TEXT NOT NULL,
    voice_id   TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS key_voice_owner ON key_voice (owner_id);
