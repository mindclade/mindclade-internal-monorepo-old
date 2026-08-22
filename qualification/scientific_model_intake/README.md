# Scientific model intake qualification

This package makes one deterministic, read-only decision: whether reviewed scientific contracts
are complete enough to begin implementation. An accepted decision has the fixed scope
`implementation-only`; it is not model registration, qualification, release approval, maturity
promotion, or deployment authorization.

## Required evidence

The closed `mindclade.dev/scientific-model-intake/v1` manifest binds the v2 target card,
scientific semantics, preprocessing and checkpoint contracts, a reference-vector pack with
predeclared tolerances, disjoint real training/evaluation dataset manifests, the evaluation
policy, serving contract and at least one runtime consumer, the safety/use policy, source and
active policy digests, and immutable approval attestations.

Modeling, data, evaluation/training, runtime, platform-control, release, and security approvals
are mandatory. Biosecurity is additionally mandatory when the intake declares that review
applicable. All approvals attest the target-card digest; duplicate roles or artifact digests fail
closed.

## Resolver contract

The composition root injects an authorization-aware, read-only resolver. It must verify payload
bytes against the requested digest before returning an identity, and it must not resolve mutable
aliases. The gate compares digest, size, media type, logical kind, and schema version and parses
the resolved target-card document through the strict Python v2 contract.

Resolver outages are infrastructure failures and propagate to the caller; they are not converted
to scientific rejection decisions. A normal not-found result is a deterministic
`unresolved-artifact` rejection.

## Explicit limits

- v1 target cards may be read for compatibility but cannot authorize new implementation.
- templates, post-hoc/non-designed target cards, missing vectors or runtime consumers,
  train/evaluation leakage, unresolved identities, and incomplete approval sets are rejected.
- the gate performs no network fallback and owns no registry, maturity, release, cloud, or
  deployment client.
- acceptance does not establish scientific correctness, numerical parity, hardware qualification,
  safety approval, or production readiness; those remain later evidence gates.
