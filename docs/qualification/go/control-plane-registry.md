# Control-plane registry qualification

**Qualified:** 2026-08-20
**Owner:** platform-control

The registry role is composed at the service boundary accepted by ADR-0010 and
ADR-0015: reusable policy lives in `control/registry`, the role factory binds it
to `internal/store/postgres`, and the HTTP adapter consumes only domain
interfaces. Generic provider packages remain mechanism-only.

## Connected environment

- PostgreSQL `17.11-alpine3.24`, multi-platform image digest
  `sha256:18cfe3ef5e6815560c98237d6216d1e5119702fb0f3894c8785dd58b8bbe5d73`.
- Go `1.26.6` with `github.com/lib/pq` from the locked root module.
- Isolated schema per test, append-only production DDL, serializable release
  transaction, and cleanup through a separate connection.

## Evidence

The connected registry suite passed:

- sealed descriptor insert, idempotent republish, and full JSONB round trip;
- evidence graph and release record committed in one transaction;
- injected failure after graph write left no partial evidence row;
- closed database pool surfaced a retryable `Unavailable` fault;
- expired lease replaced by a higher fence and the stale owner was rejected.

The declared control-plane failure matrix also passed exact race-enabled tests
for database loss, transaction rollback, lease loss, duplicate event replay,
and retry exhaustion. CI reproduces both under
`go-postgres-qualification` with the PostgreSQL image pinned by digest.

Local reproduction:

```bash
export MINDCLADE_TEST_POSTGRES_DSN='postgres://postgres:postgres@127.0.0.1:5432/mindclade_test?sslmode=disable'
go test -race -count=1 -v ./services/control_plane/internal/store/postgres
python3 tools/qualification/failure_injection.py --execute --owner control-plane
```

This evidence qualifies the materialized model/release registry boundary. It
does not qualify the webhook, projection, or housekeeping domain policies, and
it does not qualify `control/scheduling` or `control/ingestion`. Both remain
`implemented` in `components.toml`. `control/ingestion` carries no qualification
evidence of its own — unqualified for want of evidence, not for want of an
implementation. `control/scheduling` now has evidence of its own in
`docs/qualification/go/control-scheduling.md`; it is simply not this document's,
and that evidence does not advance its status either. It is not a production
release signature.
