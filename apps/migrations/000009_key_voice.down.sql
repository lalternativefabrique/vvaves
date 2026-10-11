DROP TABLE IF EXISTS key_voice;

CREATE TABLE IF NOT EXISTS default_voice (
    singleton  BOOLEAN PRIMARY KEY DEFAULT true CHECK (singleton),
    voice_id   TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
