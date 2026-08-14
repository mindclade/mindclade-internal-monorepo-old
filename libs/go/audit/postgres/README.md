# PostgreSQL audit recorder

`audit/postgres` is the canonical durable recorder for Go control-plane writes.
It joins `storage/sql/transaction` when present, making the domain mutation,
audit event, idempotency record, and transactional outbox entry one atomic
commit. Replaying an identical event ID is accepted; conflicting content is
rejected.
