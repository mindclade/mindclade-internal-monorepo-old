# AI Gateway admission qualification

**Qualified:** 2026-08-21  
**Owner:** platform-control

The authoritative AI Gateway accounting boundary is implemented in
`control/admission`, bound to PostgreSQL by
`services/control_plane/internal/store/postgres/admission`, mounted by the API
role, and expired by the leader-gated maintenance role. MLflow's Redis-backed
budget remains a secondary local guard and is not accounting authority.

## Connected environment

- Local PostgreSQL 18.4 with an isolated schema per test and `lib/pq` from the
  locked root module.
- Go 1.26.5 on Darwin arm64 with the race detector enabled.
- CI is configured to repeat the registry and admission suites on the
  digest-pinned PostgreSQL 17 image in `go-postgres-qualification`; the
  protected CI run remains the merge evidence.

## Evidence

The connected suite passed:

- sealed entitlement and budget publication with audit and outbox records in
  the same transaction;
- reservation creation, exact idempotent replay, compare-and-swap commit, and
  full JSONB round trip;
- reservation event redaction of provider payloads, request digests, and
  idempotency keys;
- 64 simultaneous contenders against a ten-request budget, producing exactly
  ten durable reservations, 54 `budget_exhausted` decisions, and no overspend;
- bounded materialized expiry followed by successful reuse of the released
  capacity.

The live backend also exposed a real-clock outbox defect that deterministic
tests could not: the caller sampled `available_at` immediately before the
factory sampled `created_at`. The adapter now lets the outbox factory assign
one coherent timestamp, preserving `available_at >= created_at`.

The maintenance unit/race suite additionally proves strict versioned payload
decoding, bounded batch draining, stable UUIDv7 work identity per five-second
UTC bucket, and idempotent restart/leadership replay. The scoped Bazel suite and
the repository static architecture contracts passed after this wiring.

Local reproduction:

```bash
export MINDCLADE_TEST_POSTGRES_DSN='postgres://postgres@127.0.0.1:5432/postgres?sslmode=disable'
go test -race -count=1 -v \
  ./services/control_plane/internal/store/postgres \
  ./services/control_plane/internal/store/postgres/admission
go test -race ./services/control_plane/internal/providers/maintenance
```

This evidence qualifies durable admission accounting and expiry. It does not
qualify policy administration, a bypass-proof Gateway proxy, provider-call
reconciliation, cross-pod lease failover, backup/restore, an SLO/runbook, or a
production release. MLflow client ingress therefore remains disabled.
