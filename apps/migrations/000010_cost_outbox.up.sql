CREATE TABLE IF NOT EXISTS cost_outbox (
    id               BIGSERIAL PRIMARY KEY,
    code             TEXT NOT NULL,
    quantity         BIGINT NOT NULL CHECK (quantity > 0),
    occurred_at      TIMESTAMPTZ NOT NULL,
    idempotency_key  TEXT NOT NULL UNIQUE,
    external_user_id TEXT NOT NULL DEFAULT '',
    attempts         INT NOT NULL DEFAULT 0,
    next_attempt_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_error       TEXT NOT NULL DEFAULT '',
    sent_at          TIMESTAMPTZ,
    parked_at        TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS cost_outbox_due ON cost_outbox (next_attempt_at)
    WHERE sent_at IS NULL AND parked_at IS NULL;
