CREATE TABLE IF NOT EXISTS mindclade_outbox (
    message_id TEXT PRIMARY KEY,
    topic TEXT NOT NULL,
    partition_key TEXT NOT NULL DEFAULT '',
    content_type TEXT NOT NULL,
    payload BYTEA NOT NULL,
    headers JSONB NOT NULL DEFAULT '{}'::jsonb,
    request_metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL,
    available_at TIMESTAMPTZ NOT NULL,
    state TEXT NOT NULL CHECK (state IN ('pending','claimed','published','dead_letter')),
    version BIGINT NOT NULL CHECK (version > 0),
    attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    claim_owner TEXT,
    claim_token TEXT,
    claimed_at TIMESTAMPTZ,
    claim_expires_at TIMESTAMPTZ,
    published_at TIMESTAMPTZ,
    dead_at TIMESTAMPTZ,
    last_error TEXT,
    CHECK ((state = 'claimed') = (
        claim_owner IS NOT NULL AND claim_token IS NOT NULL AND
        claimed_at IS NOT NULL AND claim_expires_at IS NOT NULL
    ))
);

CREATE INDEX IF NOT EXISTS mindclade_outbox_pending_idx
    ON mindclade_outbox (available_at, message_id)
    WHERE state = 'pending';

CREATE INDEX IF NOT EXISTS mindclade_outbox_claim_idx
    ON mindclade_outbox (claim_expires_at, message_id)
    WHERE state = 'claimed';
