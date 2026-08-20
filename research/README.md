<p align="center">
  <a href="../README.md"><img src="../.github/assets/brand/mindclade-wordmark.png" alt="Mindclade" width="240"></a>
</p>

[← Repository home](../README.md) · [Architecture](../docs/README.md) · [Maturity](../SCAFFOLD_STATUS.md)

# Research workspace

> **Maturity:** Non-production and scaffolded by design.
> **Primary implementation:** Python and notebooks consuming production
> packages.

`research/` is the isolated workspace for experiments, notebooks, and
benchmarks that are not part of a production dependency path.

## What's here

| Path | Responsibility |
| --- | --- |
| [`experiments/`](experiments/) | Reproducible experimental code and configurations |
| [`notebooks/`](notebooks/) | Interactive exploration and analysis |
| [`benchmarks/`](benchmarks/) | Research comparisons that are not release qualification |

## Boundary

- Research may import production packages; production packages must never
  import research code.
- A notebook result is not qualification evidence unless it is promoted into a
  deterministic, reviewed test or qualification lane.
- Do not place private datasets, patient information, hidden evaluation
  material, credentials, or model-weight secrets in this tree.
- Reusable behavior moves to the owning package only after its contract,
  ownership, tests, and dependency direction are explicit.

## Start here

- Read [`notebooks/README.md`](notebooks/README.md) before adding interactive
  work.
- Use [`docs/architecture/dependency-rules.md`](../docs/architecture/dependency-rules.md)
  for the one-way dependency boundary.
- Follow [`CONTRIBUTING.md`](../CONTRIBUTING.md) for data, security, and
  scientific-integrity expectations.
