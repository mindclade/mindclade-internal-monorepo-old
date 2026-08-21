# Admission and Gateway Budgets

## Owns

This package is the authoritative policy and accounting boundary in front of
MLflow AI Gateway. It owns:

- exact subject/workspace/provider/model entitlements;
- integer request, input-token, output-token, and cost-micro budgets;
- policy-epoch and resource-version checks;
- idempotent, expiring reservations and bounded actual-usage commits; and
- deterministic reservation versions suitable for audit and reconciliation.

Admission fails closed when entitlement or budget state is absent, stale,
inactive, or exhausted. Provider requests are represented only by a SHA-256
digest; prompts, responses, credentials, and provider tokens never enter this
domain model.

## Does not own

- MLflow authentication, workspaces, Gateway endpoint configuration, or its
  best-effort budget counters;
- Mindclade model registration, release, rollout, serving, or artifact truth;
- provider credentials or request/response payloads; or
- local node/GPU admission and tensor-memory estimation.

MLflow receives restricted telemetry and lineage projections after an
authoritative Mindclade decision. A successful MLflow request does not imply a
Mindclade release or deployment authorization.

## Transaction contract

`Repository.Reserve` must lock or compare the exact entitlement and budget
versions and account for the reservation in one serializable transaction.
`Commit` and `Release` must compare the reservation version and request digest
before making a terminal transition. Durable adapters must append the audit
record and outbox event in the same transaction as each state mutation.

The included `MemoryRepository` is a bounded, concurrency-safe reference
adapter for unit tests and local composition. The control-plane PostgreSQL
adapter lives under `services/control_plane/internal/store/postgres/admission`.
It uses serializable transactions, locks exact policy rows, persists audit and
outbox records atomically, and exposes a bounded skip-locked expiry sweep.

Deployment remains blocked until that adapter has passed connected PostgreSQL
overspend, serialization-retry, migration, reconciliation, backup/restore, and
multi-replica qualification. Unit and deterministic SQL-driver tests establish
source behavior, not a production environment claim.

Expired reservations stop consuming quota when the repository observes them.
The PostgreSQL adapter's `ExpireReservations` method materializes dormant
expired rows in bounded `FOR UPDATE SKIP LOCKED` batches and produces the same
audit/outbox events as request-driven expiration. The maintenance role still
needs to schedule that method before activation.

## Foundation consumption

The domain consumes `libs/go/clock`, `faults`, `idempotency`, `identifiers`, and
`resourceversion`. The control-plane HTTP adapter derives `Subject` from its
authenticated server context, constructs the exact idempotency scope
`<workspace>/mlflow-gateway/<subject>`, and enforces request-size limits before
hashing any provider payload.

`POST /v1/ai-gateway/reservations` creates or replays one reservation. Exact
`If-Match` mutations commit or release it. Finalization binds both the request
digest and authenticated subject, and response projections omit both the
digest and idempotency key.

Metrics should report decisions, reason codes, reservation latency, budget
utilization, expirations, and reconciliation lag with bounded labels. Logs and
traces may carry canonical IDs, route identifiers, policy versions, and quota
amounts, but never payloads or credentials.

## Verification

Run `go test -race ./control/admission` for the local concurrency contract and
`tools/dev/bazelw test //control/admission:admission_test --config=ci` for the
hermetic domain target. The SQL and HTTP adapters are covered by
`//services/control_plane/internal/store/postgres/admission:admission_test` and
`//services/control_plane/internal/providers/api:api_test`.
