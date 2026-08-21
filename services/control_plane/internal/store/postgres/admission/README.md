# PostgreSQL AI Gateway admission store

This package is the durable adapter for `control/admission.Repository`. It owns
three migration-managed tables: sealed entitlements, sealed budget windows,
and versioned reservations.

Every mutation owns a PostgreSQL serializable transaction; callers carrying an
outer transaction are rejected because its isolation and retry contract cannot
be proven. Reservation creation locks the exact
entitlement and budget rows, sums committed plus unexpired reserved usage, and
inserts only when all integer quota dimensions fit. Concurrent identical
inserts retry from a fresh serializable snapshot and replay the winner. Commit, release, and expiry
lock one reservation and compare its authenticated subject, request digest,
and resource version before a terminal transition. Audit and outbox adapters
receive the same transaction context.

The unapplied admission migrations fail on pre-existing table names instead of
silently accepting an unknown shape. Relational accounting columns are bound to
the sealed JSON document by database checks, including identity, state, quota,
time window, and resource version fields.

`ExpireReservations` uses a bounded ordered `FOR UPDATE SKIP LOCKED` batch so
multiple maintenance workers can reconcile without duplicate ownership.
Idempotent replay of an expired reservation materializes that expiration but
still returns a failed precondition; it never issues fresh authority under the
old key.

The adapter stores no prompt, response, provider credential, or provider token.
Outbox projections omit request digests and idempotency keys. The HTTP adapter
also omits those ownership proofs from responses.

Source qualification:

```sh
go test -race ./services/control_plane/internal/store/postgres/admission
nix develop .#ci --command tools/dev/bazelw test \
  //services/control_plane/internal/store/postgres/admission:admission_test \
  --config=ci
```

The connected PostgreSQL suite is part of `go-postgres-qualification` and
the local run covers real JSONB/DDL behavior, audit/outbox atomicity, event
redaction, idempotent replay, 64-way concurrent reservation pressure with an
exact budget ceiling, and expiry capacity recovery. See
`docs/qualification/go/ai-gateway-admission.md`.

Production activation still requires multi-process and multi-replica failover,
forced database loss during each mutation phase, migration rollback,
backup/restore, the enforcing Gateway proxy, policy administration, and the
operationally approved SLO/runbook. Policy provisioning must be wired to an
approved authority. Source tests exercise the scheduler loop's recurring
buckets, durable request lineage, duplicate collision checks, and bounded
seven-day terminal retention. The supporting index is append-only migration 13,
leaving already-connectable work-queue migration 5 checksum-stable. Source
wiring places that loop behind the leadership gate, but connected execution of
the current source/v13, leadership failover, and long-running retention remain
unqualified. The maintenance leader-managed work also consumes this store's
metadata-only schema readiness probe before starting and while reporting ready.
Until that evidence exists, MLflow activation remains blocked.
