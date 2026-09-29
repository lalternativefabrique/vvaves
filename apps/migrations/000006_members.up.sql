-- The account lifecycle every product core shares (urbangate ADR 0013):
-- members is the state machine, keyed by the identity id. vvaves holds no
-- row of its own for a person (their keys live at urbangate), so the local
-- id is the identity id.
CREATE TABLE IF NOT EXISTS members (
    identity_id     TEXT PRIMARY KEY,
    local_id        TEXT NOT NULL UNIQUE,
    address         TEXT UNIQUE,
    state           TEXT NOT NULL CHECK (state IN ('provisioning', 'ready', 'conflict', 'erasing', 'erased')),
    resources       JSONB NOT NULL DEFAULT '{}',
    attempts        INTEGER NOT NULL DEFAULT 0,
    last_error      TEXT NOT NULL DEFAULT '',
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    opened_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    ready_at        TIMESTAMPTZ,
    erased_at       TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS members_due_idx
    ON members (next_attempt_at)
    WHERE state IN ('provisioning', 'erasing');

INSERT INTO members (identity_id, local_id, state, opened_at, ready_at)
SELECT "identityId", "identityId", 'ready', "createdAt", "createdAt"
FROM "user"
WHERE "identityId" IS NOT NULL
ON CONFLICT (identity_id) DO NOTHING;
