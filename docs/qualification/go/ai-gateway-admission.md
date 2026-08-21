# AI Gateway admission qualification

**Source and single-process connected qualification:** 2026-08-21
**Owner:** platform-control

The authoritative AI Gateway accounting boundary is implemented in
`control/admission`, bound to PostgreSQL by
`services/control_plane/internal/store/postgres/admission`, mounted by the API
role, and wired to the maintenance role behind the source-defined leadership
gate. MLflow's Redis-backed budget remains a secondary local guard and is not
accounting authority.

## Connected environment

- Exact source snapshot `7fcbb8fd89b3d90f099fdb294f7fbc6580d450c7`
  passed locally against PostgreSQL 18.4 with an isolated schema per test and
  `lib/pq` from the locked root module.
- That run used pinned Nix Go 1.26.6 on Darwin arm64 with `-race -count=1`; all
  19 package tests passed, including seven live PostgreSQL tests.
- CI is configured to repeat the registry and admission suites on the
  digest-pinned PostgreSQL 17 image in `go-postgres-qualification`, including
  pull-request and merge-group paths. A protected connected run is still
  pending and is required merge evidence.

## Evidence

The seven connected tests passed:

- entitlement/budget publication, reservation creation and exact replay,
  compare-and-swap commit, full JSONB round trip, transaction-matched
  audit/outbox counts, and reservation-event redaction of provider payloads,
  request digests, and idempotency keys;
- forced durable-outbox rejection rolling back both the reservation mutation
  and its audit record, followed by successful reuse of the idempotency key;
- forced PostgreSQL SQLSTATE `40001` on the first insert, followed by a fresh
  serializable retry and one successful reservation;
- database rejection of resource-generation and finalization-time drift from
  each normalized sealed JSON document;
- 64 simultaneous unique-key contenders against a ten-request budget,
  producing exactly ten durable reservations, 54 `budget_exhausted` decisions,
  and no overspend;
- 32 simultaneous same-key contenders producing one reservation and one
  non-replayed creator, with transaction-matched audit/outbox cardinality; and
- bounded materialized expiry followed by successful reuse of the released
  capacity.

The live backend also exposed a real-clock outbox defect that deterministic
tests could not: the caller sampled `available_at` immediately before the
factory sampled `created_at`. The adapter now lets the outbox factory assign
one coherent timestamp, preserving `available_at >= created_at`.

The maintenance unit/race suite covers strict versioned payload decoding,
bounded batch draining, admission-schema-gated readiness/startup, successive
five-second UTC buckets, stable UUIDv7 work identity, fail-closed duplicate
payload or availability collisions, durable handler lineage, and bounded
queue-scoped retention of terminal scheduler records. These are source
behaviors, not connected evidence of multi-replica leadership failover or
long-running retention. The readiness, lineage, collision, pruning, and
migration-v13 changes in this change postdate the connected snapshot above;
their focused source/race/Bazel checks pass, but their connected
migration/worker run remains pending.

The original work-queue migration remains byte-for-byte checksum-stable at
version 5 (`16c6c1b9b95d0b4813e6f463cb4e6718bca29621892105613d54f0ecd65dd3c7`).
The terminal-retention partial index is append-only migration 13. The migration
runner verifies connected receipts before planning v13, but v13 has not yet
been applied in a connected qualification environment.

Local reproduction:

```bash
export MINDCLADE_TEST_POSTGRES_DSN='postgres://postgres@127.0.0.1:5432/postgres?sslmode=disable'
go test -race -count=1 -v \
  ./services/control_plane/internal/store/postgres \
  ./services/control_plane/internal/store/postgres/admission
go test -race ./services/control_plane/internal/providers/maintenance
```

This evidence qualifies single-process durable admission accounting and expiry
behavior. It does not qualify protected-CI execution, policy administration, a
bypass-proof Gateway proxy, provider-call reconciliation, cross-pod lease
failover, long-running retention/backlog behavior, backup/restore, an
SLO/runbook, or a production release. MLflow client ingress therefore remains
disabled.
