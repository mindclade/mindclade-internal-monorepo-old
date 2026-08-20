<p align="center">
  <a href="../README.md"><img src="../.github/assets/brand/mindclade-wordmark.png" alt="Mindclade" width="240"></a>
</p>

[← Repository home](../README.md) · [Architecture documentation](../docs/architecture/README.md) · [Decision register](../docs/design/decision-register.md)

# Enforced architecture metadata

> **Purpose:** Machine-readable ownership, dependency, and decision policy used
> by repository architecture gates.

`architecture/` contains enforceable metadata rather than narrative design
documents. Human-readable architecture lives under
[`docs/architecture/`](../docs/architecture/).

## What's here

| File | Responsibility |
| --- | --- |
| [`component_ownership.toml`](component_ownership.toml) | Component ownership and boundary metadata |
| [`dependency_budgets.toml`](dependency_budgets.toml) | Allowed dependency layers and budget policy |
| [`enforced_decisions.toml`](enforced_decisions.toml) | Accepted decisions that automated checks enforce |

## Boundary

- Change machine policy and its narrative source together.
- Do not duplicate long-form rationale in TOML; link it to the governing ADR or
  architecture page.
- A policy change must include the affected analysis checks and fixtures.

## Start here

- [Canonical system design](../docs/architecture/system-design-reference.md)
- [Dependency rules](../docs/architecture/dependency-rules.md)
- [Decision register](../docs/design/decision-register.md)
- [`tools/analysis/`](../tools/analysis/) for enforcement implementation
