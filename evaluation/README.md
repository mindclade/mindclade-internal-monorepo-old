<p align="center">
  <a href="../README.md"><img src="../.github/assets/brand/mindclade-wordmark.png" alt="Mindclade" width="240"></a>
</p>

[← Repository home](../README.md) · [Architecture](../docs/README.md) · [Maturity](../SCAFFOLD_STATUS.md)

# Evaluation and release qualification

> **Maturity:** Mixed target-state implementation; no blanket production claim.
> **Primary implementation:** Python and PyTorch.

`evaluation/` keeps model and system evaluation independent from training and
serving. It can evaluate checkpoints, model bundles, or endpoints and produces
evidence consumed by release gates.

## What's here

| Path | Responsibility |
| --- | --- |
| [`contracts/`](contracts/) | Cases, suites, metrics, results, evaluators, and reports |
| [`harness/`](harness/) | Isolated, distributed, cached, and batched evaluation execution |
| [`suites/`](suites/) and [`metrics/`](metrics/) | Capability suites and reusable measurement semantics |
| [`regression/`](regression/) and [`robustness/`](robustness/) | Baseline comparison, perturbation, and regression gates |
| [`safety/`](safety/), [`privacy/`](privacy/), and [`biological_risk/`](biological_risk/) | Risk-specific policies, checks, and reports |
| [`simulation/`](simulation/) and [`external/`](external/) | Replayable environments and external evaluation adapters |
| [`reporting/`](reporting/) | JSON, Markdown, HTML, and summary output |
| [`qualification/`](qualification/) | Thresholds, verification, promotion, and release evidence |

## Boundary

- Evaluation defines independent measurement and release evidence; it does not
  train models or own serving policy.
- Hidden sets and sensitive safety material must not leak into training,
  application code, logs, or public artifacts.
- Reusable inference mechanisms remain under [`serving/`](../serving/), while
  release authority remains in the control plane.

## Start here

- Read [`contracts/README.md`](contracts/README.md) before adding an evaluator.
- Follow [`qualification/README.md`](qualification/README.md) for promotion
  evidence.
- Use [`docs/architecture/release-evidence.md`](../docs/architecture/release-evidence.md)
  for the cross-system evidence model.

Check [`components.toml`](../components.toml) and
[`QUALIFICATION.md`](../QUALIFICATION.md) before describing a suite or gate as
qualified.
