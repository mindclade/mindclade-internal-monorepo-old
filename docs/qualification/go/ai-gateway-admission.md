# AI Gateway admission qualification

**Source and local connected qualification:** 2026-08-21
**Owner:** platform-control

The authoritative AI Gateway accounting boundary is implemented in
`control/admission`, bound to PostgreSQL by
`services/control_plane/internal/store/postgres/admission`, mounted by the API
role, and wired to the maintenance role behind the source-defined leadership
gate. MLflow's native Gateway budget and serving path remain disabled and are not
accounting authority.

## Current candidate evidence

- Migrations v15 and v16 append the atomic workspace-policy governance tables and the durable
  `reserved -> dispatched -> reconciliation_pending -> terminal` lifecycle. Existing migration
  bytes remain checksum-locked by the registry tests.
- The admin-only API accepts complete bundle proposals, exact version preconditions, and separate
  approval/rejection/cancellation decisions. Approval requires a distinct authenticated IAP
  principal and atomically writes the bundle, entitlements, budget, approval receipt, audit, and
  outbox records.
- The machine API resolves only an active subject entitlement joined to the current active bundle;
  the response contains the exact policy epoch, route, connection reference, pricing version,
  metadata-only trace posture, and subject reservation ceiling.
- Reservation admission always reserves the server-owned entitlement maximum. Caller-supplied
  quota is only a bounded declaration and cannot under-reserve the provider call.
- Local isolated Go package tests pass for admission, API/admin composition, IAP authentication,
  metrics, registry migrations, signing, middleware, configuration, and PostgreSQL adapters.
- The final package suite passed with `-race -count=1` against PostgreSQL 18.4
  for `services/control_plane/internal/providers/maintenance`,
  `services/control_plane/internal/providers/registry`, and
  `services/control_plane/internal/store/postgres/admission`. The same three
  packages passed `go vet`, and their corresponding Bazel tests passed.
- The PostgreSQL-backed maintenance test proved the bounded expiration and
  recent-lineage snapshots and required `EXPLAIN` to select each of the five
  v14 indexes. Its completed-work probe remained index-backed with 2,000 newer
  adversarial `failed` and `cancelled` rows, proving that the completed-only
  partial index avoids the mixed-terminal v13 access shape.
- CI is configured to repeat the three-package race suite on the digest-pinned
  PostgreSQL 17 image in `go-postgres-qualification` on pull-request and
  merge-group paths. A protected pull-request and merge-group run is still
  pending and is required merge evidence.

The PostgreSQL 18.4 results below are retained historical connected evidence for the earlier
accounting/observability boundary. The v15/v16 candidate still requires a fresh connected run
before activation. Neither evidence class proves that a production database has accepted the migration manifest or
that the production monitoring path has scraped and evaluated the resulting
signals.

## Historical accounting evidence

The earlier exact source snapshot
`7fcbb8fd89b3d90f099fdb294f7fbc6580d450c7` passed locally against PostgreSQL
18.4 with an isolated schema per test and `lib/pq` from the locked root module.
That run used pinned Nix Go 1.26.6 on Darwin arm64 with `-race -count=1`; all 19
package tests passed, including seven live PostgreSQL tests.

Those seven connected tests proved:

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

This historical evidence remains valid for the accounting behaviors it tested.
It predates maintenance readiness, lineage, retention, v14 observability, and
the current metric-serving boundary; the current PostgreSQL 18.4 package run
above is the evidence for those additions.

## Migration and probe integrity

Released and candidate migration bytes remain immutable. The original work
queue migration v5 remains checksum-stable at
`16c6c1b9b95d0b4813e6f463cb4e6718bca29621892105613d54f0ecd65dd3c7`.
Candidate migrations v10-v13 retain their prior names, order, and checksums:

- v10 `gateway_entitlements`:
  `45b05b578a5ccb270057a9c7deba7a0b9073fdb6a8b6e05b7faf07118ba6e887`;
- v11 `gateway_budgets`:
  `9ba9cd46aa177c1235d7a8371fac924663b43bdb7773aef5a4123e4f6355fd7f`;
- v12 `gateway_reservations`:
  `fe9bcab8c59225c4631b3837f3383707b99a610fb30493535894cc36ddd4c224`;
- v13 `work_items_terminal_retention`:
  `357900579e432566e8514df3c441433fd06c1f4161ae926039538d06e63259d2`.

Append-only v14 `gateway_admission_observability` has checksum
`7ce75981303dfbc48cfc717cc5c1bc47260a81657eabb3ebeb1a01240867313c`
and adds exactly these indexes:

- reserved reservation expiration by `expires_at,reservation_id`;
- recent admission audit events by `occurred_at,event_id`;
- recent admission outbox messages by `created_at,message_id`;
- admission outbox lookup by the `audit-event-id` header; and
- completed-only housekeeping work by `queue,completed_at,item_id`.

Append-only v15 `gateway_policy_governance` creates sealed bundle, proposal, and approval-receipt
tables with database constraints for proposal state, actor separation, bundle version lineage,
and immutable decision data. Append-only v16 `gateway_dispatch_lifecycle` expands reservations
with dispatched and reconciliation-pending states, transition timestamps, and database checks
that prevent terminal usage or timestamps from drifting from the lifecycle state.

The maintenance lineage signal is deliberately bounded to the newest 1,000
audit and outbox candidates in a 24-hour lookback. It detects recent missing or
mismatched lineage; it is not historical reconciliation and cannot prove the
absence of older drift. Production qualification therefore requires a clean
post-header 24-hour observation window in addition to any separate historical
reconciliation.

## Local reproduction

```bash
export MINDCLADE_TEST_POSTGRES_DSN='postgres://postgres@127.0.0.1:5432/postgres?sslmode=disable'
tools/dev/nixw develop .#ci --command go test -race -count=1 -v \
  ./services/control_plane/internal/providers/maintenance \
  ./services/control_plane/internal/providers/registry \
  ./services/control_plane/internal/store/postgres/admission
tools/dev/nixw develop .#ci --command go vet \
  ./services/control_plane/internal/providers/maintenance \
  ./services/control_plane/internal/providers/registry \
  ./services/control_plane/internal/store/postgres/admission
tools/dev/nixw develop .#ci-bazel --command tools/dev/bazelw test \
  --config=ci --lockfile_mode=error \
  //services/control_plane/internal/providers/maintenance:maintenance_test \
  //services/control_plane/internal/providers/registry:registry_test \
  //services/control_plane/internal/store/postgres/admission:admission_test
```

## Production activation boundary

This evidence qualifies the source-defined admission, governance, and durable lifecycle behavior
only to the evidence classes stated above. Production activation remains blocked until all of the
following have connected evidence:

- existing migration receipts are verified, v14-v16 are applied through the
  migration runner with an index-lock preflight, and the post-migration query
  plans are captured;
- the protected PostgreSQL 17 pull-request and merge-group lane passes;
- Google Managed Service for Prometheus collector identity and authorization,
  NetworkPolicy reachability, live API and maintenance scrapes, rule
  evaluation, alert routing, and notification delivery are qualified;
- the IAP audience, managed-proxy identity, and two-person administration flows receive connected
  negative and audit-lineage qualification; and
- the governed Rust Gateway proxy, provider-call reconciliation, cross-pod
  lease failover, long-running retention/backlog behavior, backup/restore, and
  production release are qualified.

MLflow client ingress therefore remains disabled.
