# Evaluation

Evaluation is independent of training because it serves training feedback,
model release, safety, registry promotion, external assessment, serving
regression, and research.

## Inputs

An evaluation can target a model bundle, checkpoint, serving endpoint, release
candidate, or external submission. Inputs and datasets are immutable references
with access policy, hidden-set controls, and environment provenance.

## Structure

```text
evaluation/contracts/     suite, task, result, evidence contracts
evaluation/harness/       isolation, batching, distributed execution
evaluation/suites/        capability and product suites
evaluation/metrics/       calibrated metric implementations
evaluation/regression/    baselines, thresholds, gates
evaluation/robustness/    perturbation and distribution-shift tests
evaluation/privacy/       memorization, leakage, membership tests
evaluation/safety/        policy and misuse gates
evaluation/biological_risk domain-specific risk screening
evaluation/external/      submission and verified import
evaluation/reporting/     JSON/Markdown/HTML evidence
evaluation/qualification/ harness qualification
```

## Reproducibility

Every result records suite/task/metric versions, model/runtime/input/dataset
and policy digests, seed and sampling plan, device/environment, kernel providers,
tolerances, raw artifacts, aggregation method, and pass/fail decision.

## Isolation

Hidden and safety datasets are accessed through scoped tickets and separate
worker identities. Model code cannot enumerate hidden-set catalogs. External
results are imported only after schema, signature, provenance, and artifact
verification.

## Release gates

Gates are declarative, versioned, and audited. A gate may require absolute
thresholds, no regression from baseline, statistical confidence, distribution
coverage, robustness, privacy, biological-risk, safety, and performance floors.
Waivers require explicit authority, expiry, reason, and immutable audit evidence.
