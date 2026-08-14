CREATE TABLE IF NOT EXISTS coordination_work_items (
 item_id TEXT PRIMARY KEY, queue TEXT NOT NULL, payload JSONB NOT NULL, priority INTEGER NOT NULL,
 available_at TIMESTAMPTZ NOT NULL, max_attempts INTEGER NOT NULL CHECK(max_attempts>0), created_at TIMESTAMPTZ NOT NULL,
 request_metadata JSONB NOT NULL DEFAULT '{}'::jsonb, state TEXT NOT NULL, attempts INTEGER NOT NULL DEFAULT 0, fence BIGINT NOT NULL DEFAULT 0,
 updated_at TIMESTAMPTZ NOT NULL, completed_at TIMESTAMPTZ, result_content_type TEXT, result_payload BYTEA, last_error TEXT,
 claim_token TEXT, claim_owner TEXT, claimed_at TIMESTAMPTZ, claim_expires_at TIMESTAMPTZ
);
