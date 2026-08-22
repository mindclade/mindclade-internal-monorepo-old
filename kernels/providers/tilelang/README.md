# TileLang kernel provider

- **Status:** Implemented source candidates; no candidate is production-qualified without connected-GPU evidence.
- **Pinned API:** TileLang `0.1.13`; apache-tvm-ffi `>=0.1.11,<0.1.13`
- **Owner:** `biology-ml`

The provider builds eager TileLang kernels lazily, so importing `kernels` does
not require an accelerator toolchain. Import and version mismatches fail with a
structured provider-unavailable error.

## Candidate families

- online-softmax dense/causal attention with tiled Q/K/V and FP32 accumulation;
- pipelined FP16/BF16/FP8 scaled GEMM with runtime scales and fused activation;
- fused SwiGLU and mask-aware Pairformer triangle multiplication;
- deterministic, padded expert-major MoE grouped GEMM;
- fused diffusion modulation, gate, and residual epilogue.

Schedules are bounded dataclasses with content digests and explicit shared
memory, alignment, dtype, thread, async-copy, TMA, WGMMA, TMEM, swizzle, and
warp-specialization legality. CUDA `sm_90`, `sm_100`, `sm_120` and HIP
`gfx90a`, `gfx942`, `gfx950` have source capability models. Runtime registration
is deliberately limited to `sm_90`; every other model remains source-only until
its generated code is inspected and qualified on connected hardware.

Compilation uses a bounded, fork-reset, single-flight LRU. Failed compilations
are never cached. Identity hashes cover the factory, invocation adapter,
semantic contract, runtime loader, schedule, TileLang pin, and TVM-FFI window.
M/N/sequence tails use guarded loads and stores; causal attention and GEMM
activations are compile-time variants rather than runtime branches.

The root Python environment stays CPU-only. Accelerator image builds consume
[`accelerator-constraints.txt`](accelerator-constraints.txt), resolve it with
the CUDA/ROCm PyTorch wheel into a hash-locked image, and bind the image digest
to qualification evidence.

## Dispatch and rollback

Candidate source, compiler version, and schedule are hashed independently. An
exact, non-revoked qualification record is mandatory before selection.
`MINDCLADE_DISABLE_TILELANG=1` forces the PyTorch fallback without rebuilding.
`MINDCLADE_DISABLE_TILELANG_OPERATIONS` accepts comma-separated exact operation
keys. Dispatch emits one structured event for either the selected candidate or
fallback, including every rejection reason.

Connected-hardware qualification must compile the exact source, inspect codegen,
run sanitizers and adversarial parity, then benchmark against the defined
baseline. Until that evidence is checked in, the candidates remain unavailable
to production dispatch by design.
