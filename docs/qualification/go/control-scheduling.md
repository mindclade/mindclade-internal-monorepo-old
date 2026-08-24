# Control-plane scheduling qualification

**Status:** `implemented` in `components.toml`. This document is evidence, not a promotion.
**Evidence recorded:** 2026-08-23
**Owner:** platform-control

The scheduling domain is `control/scheduling` — 3,799 non-test Go lines of fleet admission, fair
share, capacity ledger, placement, preemption, priority, pool and topology policy. Until this
branch its only repository was in memory. It is now also bound to PostgreSQL by
`services/control_plane/internal/store/postgres/scheduling` (2,572 non-test lines across nine
files, plus 2,097 lines of tests), whose four schemas are registered with the control-plane
migration runner as versions **25–28** (`scheduling_reservations`, `scheduling_quotas`,
`scheduling_weights`, `scheduling_ledger`), appended without renumbering anything released.

## Connected environment

Two servers appear below, and they are not the same server.

- **CI**, digest-pinned: `postgres:17-alpine` at
  `sha256:18cfe3ef5e6815560c98237d6216d1e5119702fb0f3894c8785dd58b8bbe5d73`, declared by the
  `go-postgres-qualification` job in `.github/workflows/presubmit.yml`.
- **The evidence in this document**: PostgreSQL `18.4` (Homebrew) on
  `aarch64-apple-darwin24.6.0`, a developer workstation, reached over loopback. **Every observed
  outcome recorded here was produced against 18.4. The 17-alpine run has not happened on this
  branch.** That gap matters more here than for most stores: this suite reads `pg_constraint`
  directly to find and drop a constraint by name, depends on `SERIALIZABLE` abort behaviour under
  `FOR UPDATE` contention on one hot row, and asserts a `timestamptz` round trip of the year-one
  instant. All three are server behaviour a major version is entitled to change. Do not read
  anything below as evidence about the pinned image.
- Go `1.26.6` (`darwin/arm64`) with `github.com/lib/pq` from the locked root module, `-race` and
  `-count=1` throughout.
- Isolation model: one PostgreSQL schema per live test, named `mc_sched_qual_<pid>_<n>`, created
  and dropped by the test; DDL applied from the package's own `DDL()` so the schema under test is
  the schema the migration runner registers; the conformance run instead truncates and re-seeds
  between cases and asserts the ledger returns to `fence=0, epoch=1`, so "the same starting
  state" is checked rather than assumed.

## Evidence

The connected scheduling suite ran 30 top-level tests with **0 skipped** — fifteen against a
scripted driver, fourteen live-only, and the conformance entry point — under
`ci/presubmit/live_datastore_gate.py`, which treats a skip in a declared package as an error.
Observed:

- a reservation write, its audit record, and its outbox message committed in one transaction, and
  a rejected outbox append rolled all three back;
- every projected column is held to the sealed JSONB document by a live CHECK constraint. The
  falsifiability arm is the one that makes this evidence rather than assertion: a second schema
  looks the constraint up **by name in `pg_constraint`**, requires exactly one CHECK to match,
  drops it, and then requires the identical drift to land. A constraint that had silently stopped
  existing would fail that arm, not pass the first one;
- eight concurrent admissions against one fleet snapshot admitted exactly one winner; the other
  seven were refused `fleet_snapshot_stale` and one row existed afterwards, so the ledger was not
  over-committed. Staleness is a check and not a constant refusal: a snapshot taken *after* the
  fleet moved was admitted in the same test;
- the fleet fingerprint this store rebuilds from committed rows inside the caller's transaction
  equalled the one `scheduling.NewMemoryRepository` computes from the same fleet, at three points,
  with a fixture that hits both halves of the claim-set rule — a tenant with a weight and no usage
  appears as a claim with an empty `Used` vector, a tenant with usage and no weight is absent from
  `Claims` and still counted in `Reserved`;
- the bounded expiry sweep (`MaximumExpirySweep = 64`) drained a backlog of 65 lapsed holds
  across two successive mutations without wedging. Exactly one hold remained `held` after the
  first pass, and `held` is in the ledger's occupying-state set, so the remainder went on being
  charged as occupied until it was swept — the safe direction, since over-reporting occupancy
  refuses an admission rather than over-committing one;
- a multi-victim preemption moved both victims in one transaction and replayed on re-application;
  eight concurrent binds produced one winner and seven replays;
- reducing a quota below held capacity was refused `quota_below_reserved` and moved nothing;
- a retried placement replayed on its placement key, returning the original reservation id without
  charging the ledger twice;
- reservations, the ledger epoch, and the fence were read back by a **second** `*Store` opened over
  the same tables, which is the property the whole task exists for.

### Both adapters, one suite

`control/scheduling/schedulingtest` (1,691 lines) holds the in-memory and PostgreSQL repositories
to identical behaviour across **23 cases**, each asserting the `faults` code and the reason string
together, because a reason string is API here: a caller switching on `faults.IsReason` must keep
working when the factory swaps adapters. Both adapters passed all 23 cases on this branch — the
memory adapter under `TestMemoryRepositoryConformance`, the store under `TestConformance` against
live PostgreSQL 18.4.

The suite is falsifiable in both directions. Recorded when it was written, five injected defects
each produced a named failure: in the memory adapter, building the claim set from usage instead of
recorded weights, minting an epoch on the expiry sweep, and deciding terminality before replay; in
the store, dropping `bound` from the occupying-state literals so the ledger stopped charging bound
reservations, and removing the Go-side domain sort that keeps the fingerprint from forking on a
database collation. Those five injections were performed when the suite was authored and were not
re-run to produce this document; what was re-run is both adapters, green.

### Failure matrix

Three scenarios were added to `configs/qualification/failure_injection.toml`, all
`owner = control-plane`:

| Scenario | Invariant |
|---|---|
| `control_plane_scheduling_projection_drift` | `projected_columns_cannot_drift_from_the_sealed_document` |
| `control_plane_scheduling_stale_snapshot` | `a_decision_taken_against_a_moved_fleet_cannot_commit` |
| `control_plane_scheduling_expiry_backlog` | `an_expiry_backlog_drains_across_bounded_sweeps_without_wedging` |

All three passed against 18.4 alongside the six other `control-plane` scenarios.

### The gate itself was falsified

Raising `minimum_tests` for this suite to 31 in a scratch contract produced
`LIVE-SUITE-003: control_plane_scheduling … ran 30 top-level tests, below the declared floor of
31`, and unsetting the DSN produced `LIVE-SUITE-001`. The declared floor of 30 is therefore
load-bearing rather than decorative.

Local reproduction:

```bash
export MINDCLADE_TEST_POSTGRES_DSN='postgres://postgres:postgres@127.0.0.1:5432/mindclade_test?sslmode=disable'
go test -race -count=1 -v ./services/control_plane/internal/store/postgres/scheduling
go test -race -count=1 -run TestMemoryRepositoryConformance ./control/scheduling
python3 ci/presubmit/live_datastore_gate.py
python3 tools/qualification/failure_injection.py --execute --owner control-plane
```

## What this does not qualify

This evidence supports the `implemented` status `control.scheduling` already carries. It does not
advance it, and two things are still owed before `qualified` would be honest: a **protected
pull-request run** and a **merge-group run** of `go-postgres-qualification` on the digest-pinned
`postgres:17-alpine` image. Until both exist, the only server this domain has been exercised
against is one developer's PostgreSQL 18.4. `control/admission` sits at `implemented` on exactly
this reasoning despite 4,078 lines, a live suite, and its own qualification document; this
component follows that precedent rather than arguing for an exception.

It also does not qualify the scheduler role. `services.control_plane.scheduler` remains
`experimental`, and correctly so: `WithPlacementFacts` is bound in no production composition root,
so no shipping binary composes the promotion path and **nothing in the tree writes to the
`control-plane/placement` queue yet**. The role's placement worker is real, leader-gated, and
reads its fence per item rather than once at construction — but it drains a queue that production
does not fill. A higher status would claim a readiness the code does not support.

The store's serialization budget (12 attempts, 5 ms→250 ms backoff, 5 s elapsed cap) was sized
against `placementConcurrency = 4` in the scheduler role and is argued in `config.go`. It has not
been exercised under a production placement rate, because there is not one.

Per `AGENTS.md`, the following are **reported as not run** rather than claimed: connected-CI
provider suites on the digest-pinned image, a live cluster with JobSet or Kueue CRDs (the
`control/scheduling/adapters/jobset` and `adapters/kueue` packages carry no tests at all), GPU
qualification, and remote execution. This is not a production release signature.
