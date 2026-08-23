# Artifact control SLO

**Status:** no approved objective. `control/artifacts` has no deployed process to measure.

`control/artifacts` is a domain-policy package: identity validation, access grants, retention
windows, and garbage-collection planning. It exposes no server, no transport, and no process of its
own. The only implementation of the `Catalog` seam it depends on is the in-memory
`MemoryCatalog` (`control/artifacts/repository.go:14-20`), and that type is constructed only by the
package's own tests (`control/artifacts/artifacts_test.go:16,34`). There is no durable catalog
behind this package today, so there is nothing whose availability could be observed, and no
production caller whose error budget it could consume.

Following the reasoning already recorded for `libs/rust` in `docs/slo/libs-rust.md`, this component
has no availability objective independent of the service that will eventually host the catalog.
Its objective is correctness of the policy decisions it computes.

## Unratified candidate — not an agreed target

A previous revision of this file recorded `99.9%` availability "for admitted production traffic
where applicable". That figure appeared verbatim and identically in five unrelated SLO documents
with no owner record, no measurement window, and no staging evidence. It is retained here only as
an **unratified candidate** so that the choice is not silently erased. It is not an agreed target,
it must not be quoted as one, and it cannot be ratified for this component at all until a durable
catalog exists and a named owner signs off on a measured objective.

## Correctness invariants (release-blocking, not traded for availability)

These are properties of code that exists, and hold regardless of any future numeric target.

- A digest binds to immutable metadata permanently. `Register` performs every rejection it can
  before the first write, because a half-completed registration used to leave the binding behind
  and poison the digest (`control/artifacts/service.go:11-23,24-47`).
- A location may never be recorded against an unregistered or non-matching identity
  (`artifact_location_identity_mismatch`, `control/artifacts/service.go:35`;
  `artifact_location_unknown_identity`, `control/artifacts/repository.go:52`).
- Garbage collection is fail-closed. `GCPolicy.Evaluate` returns a deletion candidate only when
  reachability, leases, pins, retention holds, the minimum-age window, and object-path/version
  validity all clear (`control/artifacts/gc.go:99-140`). Any one blocker suppresses the candidate.
- The byte plane must delete only the exact `ObjectPath` and `ObservedObjectVersion` captured when
  the plan was built, never a path reconstructed from an artifact URI
  (`control/artifacts/gc.go:63-66`).

## What must exist before any objective is set

1. A durable `Catalog` implementation, and at least one production caller of `artifacts.Service`.
2. Instrumentation. The package emits no metrics, logs, or traces; any indicator would have to be
   derived by the calling service or from the database.
3. A named owner in `OWNERS.toml` accountable for the number.

Bounded admission, cancellation, and shutdown budgets must be release-qualified before production
promotion; they are not release-qualified today.

SLO exclusions require an incident or evidence record, not an ad hoc dashboard annotation.
