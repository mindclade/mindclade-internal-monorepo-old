# Lineage

## Owns

Artifact/dataset/model/run lineage graph semantics and query policy.

## Does not own

Raw telemetry or scientific transformation implementation.

## Foundation consumption

`identifiers, pagination, storage/sql/transaction, coordination/inbox`

Durable mutations use one SQL transaction for domain state, audit, and outbox
append. Repositories expose domain-specific interfaces; concrete providers are
constructed by `services/control_plane`. Errors are structured `faults`, IDs
are canonical `identifiers`, and process lifecycle never lives in this package.

## Implemented contract

The graph is a bounded, acyclic, content-addressed input-to-output DAG. Nodes carry only a
canonical kind, SHA-256 digest, optional platform resource ID, and data classification. Mutable
paths, aliases, credentials, presigned URLs, raw samples, prompts, and artifact bytes cannot be
represented. Duplicate nodes/edges, cycles, unsupported versions, absent subjects, and invalid
identities fail closed; graph digests are stable across input ordering.

`ReleaseRequirements` proves that each required evidence kind has a path to the exact release
subject. This prevents a complete-looking but disconnected evaluation or safety artifact from
satisfying promotion policy. Repositories store the graph by its computed digest, and the service
recomputes that digest on reads to detect corruption.

`MLflowProjection` is explicitly a mirror: it carries Mindclade graph/subject digests and removes
restricted nodes plus incident edges. MLflow run IDs, model aliases, and artifact locations never
enter the authoritative graph.

The package does not implement SQL, transactions, authentication, tracing, or promotion. A
control-plane provider must persist graph state, audit, and outbox append in one transaction and
must qualify idempotency, concurrency, retention, restore, and authorization before production.
