CREATE TABLE IF NOT EXISTS key_client (
    key_id     TEXT PRIMARY KEY,
    name       TEXT NOT NULL,
    revoked_at TIMESTAMPTZ,
    changed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    synced_at  TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS key_client_unsynced ON key_client (changed_at)
    WHERE synced_at IS NULL OR synced_at < changed_at;

ALTER TABLE cost_outbox ADD COLUMN IF NOT EXISTS client_name TEXT NOT NULL DEFAULT '';
