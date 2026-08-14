# Storage outbox compatibility façade

`storage/outbox` is the stable storage-facing path reserved by the production
blueprint. It delegates to the canonical implementation in
`coordination/outbox`; it does not maintain a second state machine.

Use it when a repository or transactional unit should expose outbox storage as
part of its public dependency set. Use `coordination/outbox` directly when
constructing dispatchers, projectors, or workflow loops.

The shared behavior includes immutable validated envelopes, transactional
append, bounded fenced claims, lease renewal, stale-worker rejection,
rescheduling, dead-letter transitions, retry-aware dispatch, deterministic
in-memory conformance, and a PostgreSQL adapter using caller transactions.

No package in this namespace owns domain event schemas, broker policy, tenant
entitlements, or exactly-once claims.
