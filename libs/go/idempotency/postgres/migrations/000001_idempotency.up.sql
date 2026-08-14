CREATE TABLE IF NOT EXISTS mindclade_idempotency_records (
    identity_digest  text PRIMARY KEY,
    scope            text NOT NULL,
    idempotency_key  text NOT NULL,
    record_id        text NOT NULL UNIQUE,
    fingerprint      text NOT NULL,
    state            text NOT NULL CHECK (state IN ('in_progress', 'completed')),
    result           jsonb,
    request_id       text,
    created_at       timestamptz NOT NULL,
    updated_at       timestamptz NOT NULL,
    expires_at       timestamptz NOT NULL,
    lease_token      text,
    lease_expires_at timestamptz,
    version          bigint NOT NULL CHECK (version > 0),
    CHECK (
        (state = 'in_progress' AND lease_token IS NOT NULL AND lease_expires_at IS NOT NULL AND result IS NULL)
        OR
        (state = 'completed' AND lease_token IS NULL AND lease_expires_at IS NULL AND result IS NOT NULL)
    )
);

CREATE INDEX IF NOT EXISTS mindclade_idempotency_expiry_idx
    ON mindclade_idempotency_records (expires_at);
