CREATE TABLE IF NOT EXISTS mindclade_audit_events (
    event_id TEXT PRIMARY KEY,
    event_digest TEXT NOT NULL,
    schema_version INTEGER NOT NULL,
    occurred_at TIMESTAMPTZ NOT NULL,
    action TEXT NOT NULL,
    outcome TEXT NOT NULL,
    tenant_id TEXT NULL,
    organization_id TEXT NULL,
    actor_kind TEXT NOT NULL,
    actor_subject TEXT NOT NULL,
    target_type TEXT NOT NULL,
    target_id TEXT NULL,
    request_id TEXT NULL,
    event_json JSONB NOT NULL,
    inserted_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp()
);
CREATE INDEX IF NOT EXISTS mindclade_audit_events_occurred_at_idx ON mindclade_audit_events (occurred_at DESC);
CREATE INDEX IF NOT EXISTS mindclade_audit_events_tenant_idx ON mindclade_audit_events (tenant_id, occurred_at DESC);
CREATE INDEX IF NOT EXISTS mindclade_audit_events_target_idx ON mindclade_audit_events (target_type, target_id, occurred_at DESC);
