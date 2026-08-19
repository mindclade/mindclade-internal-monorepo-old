# Control plane production readiness

The reusable Go foundation and role contracts are implemented, and every role
now wires a service-owned provider factory. The service is still **not
promotable**: the composition layer is complete, the domain layer is not, and
several gates below need evidence that does not exist yet.

Status is per-item and repository-wide unless a row says otherwise. A box is
ticked only when something in the repository proves it.

## Required for each role

- [x] **Real PostgreSQL pool and migrations wired through `storage/sql/postgres`**
      Every role builds its pool through `providers.NewDatabase`. The `registry`
      role owns the migration manifest — audit, idempotency, outbox, leases,
      work items, cursors — because one database holds every adapter's tables
      and the version ordering must be global. No other role runs a runner.

- [~] **Transactional audit, idempotency, and outbox adapters qualified**
      Wired for every role that needs them, through `providers/durable`, and
      unit-tested. Not qualified against a live PostgreSQL: the conformance
      suites in `libs/go/storage/sql/sqltest` and the `*/postgres` adapters
      need a connected environment that CI does not currently run.

- [x] **Role-specific coordination loops use fenced claims and bounded queues**
      Work queues claim under a fencing token with bounded concurrency,
      heartbeat renewal, and dead-lettering. Electors renew well inside their
      TTL. The projector advances its cursor under the elector's fence, so a
      demoted leader cannot overwrite its successor.

- [~] **Domain engines and repositories implemented outside `libs/go`**
      **Still the gate that matters.** Repositories: `internal/store/postgres`
      implements the two contracts `control/registry` declares —
      `models.Repository` and `releases.Repository` — with DDL, content-addressed
      insert-if-absent for descriptors, and compare-and-swap on the release's own
      `ResourceVersion`. Unit-tested against `sqltest`, not against a live
      database, and **not yet wired into any role**: nothing constructs a `Store`
      (see the boundary note below). Engines: not started. Every role that
      performs work still exposes an injectable handler whose default fails
      closed — placement, projection, delivery, staging, housekeeping. A deployed
      process today assembles, validates, starts, and then refuses the work
      itself. See the domain-seam table in `GO_FOUNDATION_CONSUMPTION.md`.

      Wiring is deliberately deferred, not overlooked. `internal/providers/doc.go`
      states that no repositories or business services are assembled in the
      composition root, so constructing the registry `Store` there would
      contradict a documented boundary. Either that boundary is revised or a
      Layer 5 composition point owned by `control/registry` takes the wiring;
      that decision is open.

- [x] **Authentication and authorization provider qualified where required**
      `providers/apikeys` resolves the service credential registry with
      constant-time comparison, rejecting duplicate subjects and shared
      secrets; `auth.PermissionAuthorizer` gates every surface. Required by the
      `api`, `admin`, and `registry` roles only, and refused at startup when
      unconfigured.

- [x] **Kubernetes, blob, cache, and transport adapters wired as required**
      Kubernetes through `providers/cluster` (client, discovery, and a manager
      for the reconciling roles); blob and cache through `providers/objects`;
      HTTP, Connect, and gRPC through `internal/transport`. Each role links
      only what its profile justifies, enforced by `profile_test.go`.

- [~] **Readiness, liveness, drain, cancellation, and shutdown tests pass**
      The staged lifecycle, probes, and bounded reverse shutdown are tested in
      `libs/go/servicekit`. One prober answers for HTTP, Connect, and gRPC so
      the three surfaces cannot disagree. What is missing is control-plane
      level coverage: `services/control_plane/tests/` still holds four
      scaffold placeholders, not real drain or shutdown tests.

- [ ] **Failure injection covers database loss, lease loss, duplicate events, and retry exhaustion**
      **Not done.** No failure-injection harness exists in the repository.
      `libs/go/faults` models and classifies failures; it does not inject them.

- [ ] **SLOs, dashboards, alerts, and runbooks linked**
      **Not done.** No SLO or runbook artifacts are present to link.

- [ ] **Bazel build, SBOM, provenance, image signature, and rollback evidence attached**
      **Blocked.** `bazel test //...` cannot run: every rules_go release in the
      registry passes `GOEXPERIMENT=coverageredesign`, which the pinned Go
      rejects, so Bazel compiles against 1.22.7 while `go.mod` pins 1.26.0. The
      analysis is in `MODULE.bazel`. All BUILD targets in this service are
      hand-written and unverified against a real Bazel analysis.

- [x] **`bootstrap.UnconfiguredFactory` absent from the promoted command**
      Enforced, not reviewed: `internal/bootstrap/promotion_test.go` fails if
      any command references it, skips `bootstrap.Main`, or constructs a
      service directly, and if any role lacks a command directory.

## Summary

| | Count |
|---|---:|
| Satisfied | 6 |
| Partial | 3 |
| Not done | 1 |
| Blocked on toolchain | 1 |

**The critical path to promotion is domain implementation.** Everything the
foundation owns is wired and guarded. The registry's storage layer now exists
but is unreachable, and no domain engine does. The next step is the composition
decision that lets a `Store` be constructed at all; after that, in order:
domain engines behind the fail-closed seams, control-plane lifecycle tests, a
live-PostgreSQL qualification run, failure injection, then the operational
artifacts. The Bazel gate is independent and blocked upstream.
