<p align="center">
  <a href="../README.md"><img src="../.github/assets/brand/mindclade-wordmark.png" alt="Mindclade" width="240"></a>
</p>

[← Repository home](../README.md) · [Dependency rules](../docs/architecture/dependency-rules.md) · [Maturity](../SCAFFOLD_STATUS.md)

# Shared language libraries

> **Maturity:** Mixed by language and package; use component-level evidence.
> **Role:** Reusable mechanisms with multiple independent consumers.

`libs/` contains reusable, language-specific mechanisms. Domain policy belongs
in owning domain packages, and deployable composition belongs under
[`services/`](../services/).

## Language roots

| Path | Responsibility | Current orientation |
| --- | --- | --- |
| [`go/`](go/) | Control-plane mechanisms, lifecycle, storage, coordination, transport, and observability | Qualified foundation; read its evidence and admission rules |
| [`rust/`](rust/) | Runtime, storage, protocol, IPC, worker, telemetry, and safety mechanisms | Implemented foundation with component-specific qualification |
| [`python/`](python/) | Scientific configuration, artifacts, distributed behavior, serialization, and worker mechanisms | Mixed target-state implementation |
| [`ts/`](ts/) | API, auth, charts, design-system, molecular-viewer, and telemetry packages | Product-facing package boundaries with component-specific maturity |

## Boundary

- A mechanism enters `libs/` only after at least two independent consumers
  demonstrate the shared need.
- Libraries do not own business/domain policy or provider-specific service
  composition.
- Cross-language data uses [`protocols/`](../protocols/), not duplicated private
  types.
- Avoid generic `common`, `shared`, `helpers`, and `utils` dumping grounds.

## Start here

- [`go/README.md`](go/README.md), [`go/LAYERS.md`](go/LAYERS.md), and
  [`go/USAGE.md`](go/USAGE.md)
- [`rust/README.md`](rust/README.md), [`rust/LAYERS.md`](rust/LAYERS.md), and
  [`rust/PACKAGE_CATALOG.md`](rust/PACKAGE_CATALOG.md)
- [`python/README.md`](python/README.md)
- [`ts/README.md`](ts/README.md)

Confirm package maturity in [`components.toml`](../components.toml) and required
evidence in [`QUALIFICATION.md`](../QUALIFICATION.md).
