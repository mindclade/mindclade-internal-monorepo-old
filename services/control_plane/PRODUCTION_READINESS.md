# Control plane production readiness

Readiness is evaluated per deployable role. The registry role is no longer
blocked on a composition decision: its model and release engines are wired to
the production PostgreSQL store and qualified against live PostgreSQL. Other
roles with explicit fail-closed domain seams remain unavailable and must not be
promoted merely because the registry is ready.

## Registry role

- [x] **Production composition boundary decided and enforced.** The shared
      `internal/providers` package remains mechanism-only. A role package under
      `internal/providers/<role>` is a Layer-5 process composition root and may
      bind reusable `control/` services to concrete repositories and transports.
      `internal/providers/registry` owns that binding for the registry.

- [x] **Model and release domain engines wired.** `RegistryFactory.Create`
      constructs `internal/store/postgres.Store`, `models.Service` with its
      production publication policy, and `releases.Service` with its production
      promotion policy. Evidence graph and release persistence share one
      serializable transaction.

- [x] **Registry HTTP surface is authenticated and fail-closed authorized.**
      Publication, resolution, and promotion have explicit permissions. The
      middleware requires a mapping, so an added route is denied until its
      authorization target is declared.

- [x] **Production DDL is migration-owned and append-only.** Registry descriptor,
      release, and evidence-graph migrations follow the shared durable adapter
      migrations. Only the registry role runs the global manifest.

- [x] **Live PostgreSQL and failure injection are qualified.** The connected
      suite covers JSONB round trips, idempotent content-addressed publication,
      atomic release promotion, rollback after an injected mid-transaction
      failure, retry classification after database loss, and stale-owner lease
      rejection. The CI service image is pinned by digest. See
      `docs/qualification/go/control-plane-registry.md`.

- [x] **SLO and runbook contracts exist.** Availability, latency, RPO/RTO, burn
      response, rollback, and database-loss procedures are defined in
      `docs/slo/control-model-registry.md` and
      `docs/runbooks/control-model-registry.md`.

- [x] **Repository-wide Bazel passes and is a required gate.** The 2026-08-20
      local qualification analyzed 1,079 targets and all 304 tests passed with
      lockfile drift rejected. Presubmit runs the same complete repository
      graph through `tools/dev/bazelw test //... --config=ci`; scoped Go results
      are not a substitute.

- [x] **Release supply-chain controls are materialized.** The release workflow
      uses commit-pinned reusable workflows to build both images, emit SPDX SBOMs
      and SLSA provenance, sign and attest images, and verify signatures before
      accepting rollback evidence. A dry workflow dispatch publishes nothing.
      Actual signatures and attestations are release artifacts and therefore
      exist only after an authorized tagged or push-enabled release succeeds.

The registry role may advance through the normal presubmit and release gates.
Promotion still requires the release run's SBOM, provenance, signature, and
rollback artifacts; repository code does not manufacture evidence for a release
that did not happen.

## Roles still held fail-closed

| Role | Remaining domain or provider gate |
|---|---|
| `api`, `admin` | AI Gateway reservation/create/commit/release is mounted with fail-closed authz, durable storage, schema readiness, and source-owned SLO/runbook contracts. A single-process connected PostgreSQL suite covers no-overspend and expiry-capacity recovery; protected-CI, multi-process/multi-replica, failure-injection, and restore evidence remain, as do policy administration, an enforcing Gateway proxy, and operational SLO approval. Other business APIs are not yet mounted. |
| `scheduler` | Placement handler is not configured. |
| `controller`, `operator` | Domain reconcilers are not registered. |
| `event-projector` | Projection source and handler are not configured. |
| `event-dispatcher` | A production Pub/Sub adapter is not present. |
| `webhook-dispatcher` | Delivery handler is not configured. |
| `ingestion-controller` | Staging handler is not configured. |
| `maintenance` | Source tests cover admission-schema-gated readiness/startup, successive recurring buckets, collision-safe idempotency, durable handler lineage, bounded terminal retention, and bounded skip-locked expiry batches; source composition places the worker/scheduler behind the leadership gate. Migration v5 stays checksum-stable and the retention index is append-only v13. Protected connected execution of the current source/v13, multi-replica lease failover, long-running backlog/retention behavior, other housekeeping policies, and operational SLO approval remain. |

These are independent promotion units, not hidden registry dependencies. Each
must gain its remaining concrete domain composition, connected qualification,
SLO, and runbook before its deployment is enabled. Incomplete seams retain
stable `*_not_configured` faults so they cannot report successful work; the
maintenance Gateway-expiry seam is no longer one of those placeholders.

## Cross-role enforcement

- `internal/bootstrap/promotion_test.go` requires every command to enter through
  `bootstrap.Main` and forbids `bootstrap.UnconfiguredFactory` in promoted
  commands.
- `internal/bootstrap/profile_test.go` rejects provider capabilities that a
  role's declared profile does not justify.
- `tools/analysis/check_foundation_consumption.py` and
  `tools/analysis/check_go_layers.py` enforce foundation consumption and the Go
  dependency law.
- The control-plane failure matrix names database loss, transaction rollback,
  lease loss, duplicate replay, and retry exhaustion explicitly; CI executes
  the control-plane subset rather than accepting placeholder scenarios.
- Singleton work loops are bound to the foundation leadership handler. Unit
  tests prove standby scheduler, projector, and controller components have no
  independent `Run` path, and lease-loss tests prove configured electors fail
  stop rather than reusing a canceled loop. Connected multi-replica failover
  and stale-leader rejection remain required before any held role is promoted.
