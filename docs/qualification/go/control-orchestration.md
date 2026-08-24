# Control-plane orchestration qualification

**Status:** `implemented` in `components.toml`. This document is evidence, not a promotion.
**Evidence recorded:** 2026-08-23
**Owner:** platform-control

The orchestration domain is `control/orchestration` — 2,688 non-test Go lines of workflow
compilation, stage state machine, attempt lifecycle, dependency graph, and cancellation policy.
It is bound to PostgreSQL by `services/control_plane/internal/store/postgres/orchestration`
(1,694 non-test lines), and its outbox payloads are typed by
`protocols/proto/mindclade/orchestration/v1/orchestration_events.proto`. The composition is the
one ADR-0010 and ADR-0015 accept: domain policy in `control/`, the durable binding in the
service's `internal/store`, and no orchestration algorithm inside a provider package.

## Connected environment

Two servers appear below, and they are not the same server.

- **CI**, digest-pinned: `postgres:17-alpine` at
  `sha256:18cfe3ef5e6815560c98237d6216d1e5119702fb0f3894c8785dd58b8bbe5d73`, declared by the
  `go-postgres-qualification` job in `.github/workflows/presubmit.yml`.
- **The evidence in this document**: PostgreSQL `18.4` (Homebrew) on
  `aarch64-apple-darwin24.6.0`, a developer workstation, reached over loopback. **Every observed
  outcome recorded here was produced against 18.4. The 17-alpine run has not happened on this
  branch.** These are different major versions, and this suite depends on server behaviour a
  major version is entitled to change — CHECK-constraint enforcement, `SERIALIZABLE` conflict
  detection under concurrent transitions, and the JSONB round trip. Nothing below should be read
  as evidence about the pinned image.
- Go `1.26.6` (`darwin/arm64`) with `github.com/lib/pq` from the locked root module, `-race` and
  `-count=1` throughout.
- Isolation model: one PostgreSQL schema per live test, named `mc_orch_qual_<pid>_<n>`, created
  and dropped by the test; DDL applied from the package's own `DDL()` so the schema under test is
  the schema the migration runner registers; cleanup through a separate connection.

## Evidence

The connected orchestration suite ran 23 top-level tests with **0 skipped** — nine of them
live-only — under `ci/presubmit/live_datastore_gate.py`, which treats a skip in a declared
package as an error. Observed:

- a stage write, its audit record, and its outbox message committed in one transaction, and an
  outbox append that was rejected rolled the domain row and the audit record back with it;
- a projected `state` column updated to disagree with the JSONB document it was derived from was
  refused by a live CHECK constraint;
- a published workflow was immutable: republishing the identical definition replayed, and
  redefining the same workflow id raised `conflict`;
- racing reconcilers produced exactly one durable transition; a stale resource version was
  refused; a stale attempt generation could not overwrite a newer one;
- a redelivered cancellation replayed on its idempotency identity rather than reapplying;
- `GetStages` returned only the stages that were actually materialized, so a caller cannot read
  an absent stage as a present one;
- a promotion enqueued its stage on the `control-plane/placement` queue **inside the transition's
  own transaction**: the item carried the promoted stage's run, job, and stage identifiers at
  attempt 1, a replayed promotion placed nothing a second time, and — the discriminating case — a
  promotion whose placement was appended successfully and whose mutation then failed left **zero**
  work items, the stage still `blocked`, its resource version unmoved, and the outbox count
  unchanged. The earlier version of that subtest installed `CHECK (false)` on the queue table,
  which fails an insert on any connection and therefore passed whether or not the append had
  joined the transaction; it was replaced because it could not fail for the reason it existed.

Three further properties are checked against a scripted driver rather than a live server,
because what they assert is the statement stream or the constructor and not the server's
behaviour:

- the stage version seal is computed by the domain and never restated by the store —
  `TestStageDigestMatchesTheMemoryAdapter` drives the same two transitions through both adapters
  and compares `Version.String()`, `State`, `Attempts`, and `UpdatedAt`;
- a store composed with no placement producer still records stage state, and a *nil* producer is
  refused at construction, because a typed nil that silently dropped every placement is
  indistinguishable from the defect the transaction exists to prevent;
- `GetStages` binds every identifier it is given rather than interpolating it, refuses an
  oversized lookup, and short-circuits an empty list without issuing a query.

Outbox payloads are now typed. `orchestration/v1` is the first domain in the repository to
declare a non-empty `payload_messages` list in `protocols/mappings/event_proto.yaml`; its four
messages mirror the structs the store marshals, and every bound in the generated schema was read
out of the Go validator that decides what the store may emit.

The declared failure matrix gained `control_plane_placement_rollback`
(`invariant = an_appended_placement_does_not_survive_a_failed_promotion`,
`owner = control-plane`). It passed against 18.4 together with the eight other `control-plane`
scenarios.

Local reproduction:

```bash
export MINDCLADE_TEST_POSTGRES_DSN='postgres://postgres:postgres@127.0.0.1:5432/mindclade_test?sslmode=disable'
go test -race -count=1 -v ./services/control_plane/internal/store/postgres/orchestration
python3 ci/presubmit/live_datastore_gate.py
python3 tools/qualification/failure_injection.py --execute --owner control-plane
```

## What this does not qualify

This evidence supports the `implemented` status `control.orchestration` already carries. It does
not advance it, and two things are still owed before `qualified` would be honest: a **protected
pull-request run** and a **merge-group run** of `go-postgres-qualification` on the digest-pinned
`postgres:17-alpine` image. Until both exist, the only server this domain has been exercised
against is one developer's PostgreSQL 18.4. `control/admission` sits at `implemented` on exactly
this reasoning despite 4,078 lines, a live suite, and its own qualification document; this
component follows that precedent rather than arguing for an exception.

It also does not qualify the roles. `services.control_plane.controller` and
`services.control_plane.operator` remain `experimental`, and correctly so: `WithStageReconciler`
is bound in no production composition root, so the leader-gated stage worker each role runs drains
its queue against a fail-closed default that refuses with `stage_reconciler_not_configured`.
Neither role carries production work yet, and a higher status would claim a readiness the tree
does not support.

Per `AGENTS.md`, the following are **reported as not run** rather than claimed: connected-CI
provider suites on the digest-pinned image, a live cluster with JobSet or Kueue CRDs (the
`control/orchestration/adapters/kubernetes` launcher is exercised only against fakes), GPU
qualification, and remote execution. This is not a production release signature.
