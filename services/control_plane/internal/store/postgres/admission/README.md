# PostgreSQL AI Gateway admission store

This package is the durable adapter for `control/admission.Repository`. It owns
three migration-managed tables: sealed entitlements, sealed budget windows,
and versioned reservations.

Every mutation runs at PostgreSQL serializable isolation unless it joins an
existing control-plane transaction. Reservation creation locks the exact
entitlement and budget rows, sums committed plus unexpired reserved usage, and
inserts only when all integer quota dimensions fit. Commit, release, and expiry
lock one reservation and compare its authenticated subject, request digest,
and resource version before a terminal transition. Audit and outbox adapters
receive the same transaction context.

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
covers real JSONB/DDL behavior, audit/outbox atomicity, response/event
redaction, idempotent replay, 64-way concurrent reservation pressure with an
exact budget ceiling, and expiry capacity recovery. See
`docs/qualification/go/ai-gateway-admission.md`.

Production activation still requires multi-process and multi-replica failover,
forced database loss during each mutation phase, migration rollback,
backup/restore, the enforcing Gateway proxy, policy administration, and the
approved SLO/runbook. Until that evidence exists, MLflow activation remains
blocked.
