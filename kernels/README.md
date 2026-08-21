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

## Implemented operation contracts

| Operation key | Reference semantics | TileLang source candidate |
| --- | --- | --- |
| `attention.sdpa` | Dense BHSD attention, causal or explicit allowed-mask semantics, FP32 softmax | Online-softmax, tiled Q/K/V, FP32 accumulation |
| `fp8.scaled_gemm` | Saturating FP8 cast plus runtime-scaled GEMM | Pipelined tensor-core GEMM with fused scale/activation epilogue |
| `fused.swiglu` | FP32 SiLU evaluation and elementwise product | Single-pass fused epilogue |
| `pairformer.triangle_incoming` / `pairformer.triangle_outgoing` | Mask-aware `[B,N,N,C]` contractions | Tiled GEMM contraction per batch/channel |
| `moe.grouped_gemm` | Deterministic padded expert-major GEMM | Expert-grid grouped GEMM |
| `diffusion.modulated_residual` | Adaptive scale/shift/gate/residual | Single-pass broadcast-free epilogue |

The source candidates are registered but cannot win dispatch merely by being
present. `KernelDispatcher` requires an exact request and implementation digest
in a non-revoked qualification manifest. Set `MINDCLADE_DISABLE_TILELANG=1` for
an immediate PyTorch rollback.

## Local verification

```bash
uv run --frozen pytest -q kernels tests/numerical/test_kernel_provider_parity.py
uv run --frozen ruff check kernels tests/numerical/test_kernel_provider_parity.py
tools/dev/bazelw test //kernels/... //tests/numerical:test_kernel_provider_parity --config=ci
```

GPU compilation, sanitizer, parity, and performance runs belong in a pinned
accelerator environment with TileLang `0.1.13`; CPU-only success does not create
qualification evidence.
