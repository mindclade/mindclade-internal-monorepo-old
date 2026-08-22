# Models / Contracts

- **Status:** Target-card core implemented; remaining model contracts are scaffolded.
- **Primary implementation ownership:** Python/PyTorch

## Purpose

Model contracts, reusable components, independent model families, adapters, references, and registry metadata. Model families do not import one another. This path specializes that domain for **contracts**.

## Boundary

Reusable implementation belongs in this owning package. Deployable entry points,
provider construction, health/drain wiring, and deployment evidence belong under
`services/`. Cross-language data exchanged outside a process uses versioned
contracts under `protocols/` rather than language-private structures.

This package must not become a `common`, `shared`, `helpers`, or `utils` dumping
ground. It may depend only in the direction documented by
`docs/architecture/dependency-rules.md` and the accepted ADRs.

## Implemented target-card and intake boundaries

`target_card.py` gives biology, diffusion, LLM, MoE, and multimodal families one fail-closed
contract for immutable input/output schemas, proprietary dataset identities, evaluation gates,
availability and hardware profiles, and activation evidence. Version 2 requires predeclared
evaluation slices and an explicit safety-review policy. Version 1 remains read-only compatibility.
A card defaults to `designed` and cannot enter `approved` without a canonical qualification-
evidence digest.

`scientific_intake.py` is the closed, provider-neutral input to the qualification gate. It binds a
target card, scientific and preprocessing semantics, checkpoint rules, reference vectors,
disjoint training/evaluation dataset manifests, evaluation and serving policies, a real runtime
consumer, safety/use policy, source and policy digests, and immutable role attestations. An intake
decision can authorize implementation only. It cannot register, qualify, approve, release, or
deploy a model, and it does not choose scientific thresholds or claim any family is implemented.

## Remaining materialization requirements

Before the remaining scaffold modules are treated as implemented, add:

- a named owner and reviewed stable contract;
- implementation with bounded resources, cancellation, and deterministic or
  explicitly statistical behavior;
- package-local tests plus required integration/numerical/security evidence;
- a Bazel target using the pinned Nix toolchain environment;
- explicit inputs, outputs, compatibility, failure, retry, and rollback rules;
- documentation of limits and non-responsibilities;
- `PRODUCTION_READINESS.md` evidence for deployment-facing code.

See the architecture chapter for this domain and `SCAFFOLD_STATUS.md` for the
artifact-wide implementation status.
