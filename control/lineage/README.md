# Lineage

## Owns

Artifact/dataset/model/run lineage graph semantics and query policy.

## Does not own

Raw telemetry or scientific transformation implementation.

## Foundation consumption

`faults, identifiers`

This package imports those two and nothing else. Repositories expose
domain-specific interfaces; the concrete provider is constructed by
`services/control_plane`. Errors are structured `faults`, IDs are canonical
`identifiers`, and process lifecycle never lives in this package.

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

`Put` binds a digest to one graph permanently. Republishing identical provenance — including a
re-encoding with reordered nodes or edges, which the canonical digest treats as the same graph —
must succeed, because a lost response leaves the caller no other recovery. Filing a *different*
graph under an existing digest must be refused: a release cites provenance by digest, so a
rebinding would silently repoint every existing citation. Both rejections carry the sentinels and
reason codes in `errors.go`, so a caller cannot tell one implementation from another by what it
gets back.

## Durability

The durable provider is `services/control_plane/internal/store/postgres`
(`Store.LineageGraphs()`), keyed by `graph_digest` and joining the caller's transaction rather
than opening its own. `services/control_plane/internal/store/postgres/live_lineage_test.go`
qualifies idempotency, the rebinding refusal at both the pre-insert check and the stored row,
transactional rollback, bounds, and the `CodeDataLoss` corruption path against a real PostgreSQL
server.

The package itself still implements no SQL, authentication, tracing, or promotion, and **nothing
in the repository constructs a lineage repository or calls `Service.Publish`** — there is no
producing caller yet. Retention, restore, authorization, and concurrency under contention remain
unqualified, so this is not a production-ready provenance record.
