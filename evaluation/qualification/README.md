# Evaluation / Qualification

- **Status:** Provider-neutral qualification core implemented; connected scientific and release
  qualification remains evidence, not a source claim.
- **Primary implementation ownership:** Python/PyTorch

## Purpose

Independent evaluation contracts, harnesses, suites, metrics, reporting, safety, privacy, robustness, and release qualification. Evaluation can run against checkpoints, bundles, or endpoints. This path specializes that domain for **qualification**.

## Boundary

Reusable implementation belongs in this owning package. Deployable entry points,
provider construction, health/drain wiring, and deployment evidence belong under
`services/`. Cross-language data exchanged outside a process uses versioned
contracts under `protocols/` rather than language-private structures.

This package must not become a `common`, `shared`, `helpers`, or `utils` dumping
ground. It may depend only in the direction documented by
`docs/architecture/dependency-rules.md` and the accepted ADRs.

## Implemented contract

`EvaluationPlan` binds a candidate, scorer, dataset snapshots, absolute thresholds,
baseline-relative regression limits, minimum sample counts, and required slices before a
candidate is measured. `build_evidence` emits one outcome for every declared rule and turns a
missing scorer output into failed evidence. NaN/Inf, duplicate metrics, undeclared metrics,
mutable identifiers, candidate/scorer mismatches, insufficient samples, and missing slices are
rejected or fail closed.

`EvaluationEvidence` is deterministic JSON with content digest, exact source/image/scorer/data
identity, aggregate outcomes, and execution failure counts. It cannot carry raw examples. Its
MLflow projection contains only bounded aggregates and immutable Mindclade references; MLflow is
never the evidence authority.

Independent verification requires the configured metric categories and an attestation bound to
the exact evidence and policy digests. The resulting `PromotionDecision` is a signed-decision
input for the Go control plane; Python cannot mutate registry aliases or deployment state.

## Operational boundary

The core is deterministic and has no I/O, network, credentials, retry loop, or unbounded
collection. Evaluation harnesses own cancellation and isolated execution. Go owns durable
promotion. GitOps owns deployment state. A successful unit test does not qualify a particular
model, scorer, holdout, judge, runtime, or environment; those require linked evidence for
determinism, leakage/contamination, slice coverage, judge calibration, safety, latency, cost,
security, and rollback.
