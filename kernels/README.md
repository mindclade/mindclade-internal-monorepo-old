<p align="center">
  <a href="../README.md"><img src="../.github/assets/brand/mindclade-wordmark.png" alt="Mindclade" width="240"></a>
</p>

[← Repository home](../README.md) · [Architecture](../docs/README.md) · [Maturity](../SCAFFOLD_STATUS.md)

# Accelerator kernels

> **Maturity:** Mixed target-state implementation; kernels are usable only for
> signatures and targets covered by current evidence.
> **Primary implementation:** Python provider APIs and TileLang kernels.

`kernels/` owns reference operations, provider dispatch, accelerator
implementations, autotuning, target support, and signature-specific numerical
and performance qualification.

## What's here

| Path | Responsibility |
| --- | --- |
| [`api/`](api/) | Provider-neutral specifications, capabilities, validation, and errors |
| [`ops/`](ops/) | Operation families such as attention, diffusion, FP8, fused, and MoE |
| [`providers/`](providers/) | PyTorch, TileLang, and vendor provider adapters |
| [`tilelang/`](tilelang/) | TileLang compiler, target, testing, and autotuning support |
| [`qualification/`](qualification/) | Numerical parity, performance, promotion, fallback, and revocation evidence |

## Boundary

- Reference semantics remain independently executable and testable.
- Dispatch must fail closed or use an explicitly qualified fallback when a
  signature, target, or runtime condition is unsupported.
- A kernel is not globally "qualified"; evidence is scoped to the operation,
  signature, dtype, hardware target, toolchain, and performance envelope.
- Model-family policy stays under [`models/`](../models/).

## Start here

- Read [`api/README.md`](api/README.md) for the provider contract.
- Read [`qualification/README.md`](qualification/README.md) before promotion or
  benchmark claims.
- Use [ADR-0008](../docs/design/adr-0008-qualified-tilelang-kernels.md) for the
  qualification boundary.

Check [`QUALIFICATION.md`](../QUALIFICATION.md) for connected hardware and
provider work that remains outstanding.
