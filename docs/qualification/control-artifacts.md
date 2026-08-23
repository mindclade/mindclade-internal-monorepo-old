# Artifact control qualification

**Candidate date:** 2026-08-23
**Owner:** platform-control
**Component:** `control.artifacts` (`control/artifacts`)
**Status:** durable-store evidence recorded. **Not qualified for production.**

## What this document is

`control/artifacts` acquired a durable PostgreSQL store and a permanently enforced
digest-to-metadata binding in commit `9be49a39` ("give the artifact catalog a durable store and a
permanent identity binding", #137, merged 2026-08-23). That work was verified against a real
PostgreSQL server rather than a skipped suite, which makes it the first connected durability
evidence this component has ever had. This file records exactly what that run covered so the
evidence is citable, and — equally — what it did not cover, so it is not mistaken for a readiness
claim.

Recording evidence is not promoting a component. `control.artifacts` remains `implemented`.

## Evidence that ran

All rows below are from the #137 change description and were run by its author against a
PostgreSQL 18.4 server started from host binaries (`initdb`/`pg_ctl`), not a container.

| Suite | Result |
|---|---|
| `control/artifacts` (domain plus in-memory reference) | pass |
| `services/control_plane/internal/store/postgres` — fake-driver query-shape tests | pass |
| `services/control_plane/internal/store/postgres` — `live_artifacts_test.go` | pass |
| `services/control_plane/internal/store/postgres` — pre-existing `live_postgres_test.go` | pass |
| `services/control_plane/internal/store/postgres/admission` | pass |

Against real SQL rather than a map, the live artifact suite asserts the immutable identity binding
across all four metadata columns; that a rejected registration writes no orphaned location row;
transactional rollback composition with the caller's unit of work; the placement-set bound as
PostgreSQL actually enforces it; and conformance between the durable store and the in-memory
reference, including the same sentinel and the same `faults` reason from both.

The headline guard was mutation-tested. Rewriting `ON CONFLICT (digest) DO NOTHING` to
`DO UPDATE SET ...` failed 4/4 subtests, and dropping the immutable-column predicate from the
`Register` identity CTE failed with `Register rebound the digest` and additionally caught
`a rejected registration wrote its location anyway`. Both mutations were reverted. The suite
therefore fails before the store exists and passes after it, which is what makes it evidence
rather than decoration.

### Re-verified for this record

`tools/dev/nixw develop .#default --command go test -race -count=1 ./control/artifacts/...` passes
on the merge base of this change (`ok go.mindclade.dev/control/artifacts`). That is the domain and
in-memory reference only; see below for why the durable half was not re-run here.

## What did NOT run, and what this does not establish

- **The live suite was not re-executed for this document.** `MINDCLADE_TEST_POSTGRES_DSN` is unset
  in this environment, and `live_postgres_test.go` skips silently when it is. A local `go test` of
  the store package here reports success while running none of the live cases — the exact failure
  mode that made every other PostgreSQL suite in this tree worthless as evidence. The live results
  above are cited from #137, not reproduced.
- **The qualified server version is not the version CI runs.** #137 ran PostgreSQL 18.4 locally.
  The `go-postgres-qualification` job in `.github/workflows/presubmit.yml` pins
  `postgres:17-alpine` by digest and runs `./services/control_plane/internal/store/postgres`, which
  contains `live_artifacts_test.go`. So the artifact store does have a live CI lane, but no run has
  qualified 18.4 and 17 as equivalent for these query shapes.
- **`control/artifacts` still has no production caller.** The durable store implements
  `artifacts.Catalog`, but nothing in a deployed process constructs it. `BuildGCPlan` has no caller
  outside the package.
- **Not covered by any connected evidence:** garbage-collection plan execution and receipt
  validation end to end; access-grant expiry behaviour under a real clock across process restarts;
  retention-window enforcement; migration receipts and post-migration query plans for the artifact
  DDL; backup and restore; and behaviour under concurrent registration load.
- **No availability objective exists.** `docs/slo/artifact-control.md` records no approved target
  and preserves an earlier `99.9%` only as an explicitly unratified candidate.

## Known documentation drift

`docs/slo/artifact-control.md` predates #137 and still states that "There is no durable catalog
behind this package today" and that the only `Catalog` implementation is the in-memory
`MemoryCatalog` constructed only by the package's own tests. Both statements were true when written
and are now false. The SLO's conclusion — that this component has no independent availability
objective and its objective is correctness of the policy decisions it computes — is unaffected,
because the durable store is hosted by the control plane rather than by `control/artifacts` itself.
Correcting that paragraph is an SLO-owner edit and is deliberately not made here.

## Gate status

| Gate | State |
|---|---|
| tests | met |
| build_target | met |
| qualification | this document |
| slo | `docs/slo/artifact-control.md` — exists, records no approved objective |
| runbook | `docs/runbooks/control-artifacts.md` — exists, states the package has no production caller |
| release_target | absent — no catalog entry releases `control/artifacts` |

Production promotion is blocked on the release target, on an approved SLO, and on the uncovered
evidence classes listed above.
