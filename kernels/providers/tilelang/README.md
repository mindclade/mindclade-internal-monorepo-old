# TileLang kernel provider

- **Status:** Implemented source candidates; no candidate is production-qualified without connected-GPU evidence.
- **Pinned API:** TileLang `0.1.13`
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
memory, alignment, dtype, thread, and async-copy legality. Registration covers
CUDA `sm_90`, `sm_100`, `sm_120` and HIP `gfx90a`, `gfx942`, `gfx950` only when
the target capability model admits the schedule. Architecture defaults are
starting points, never claimed winners.

## Dispatch and rollback

Candidate source, compiler version, and schedule are hashed independently. An
exact, non-revoked qualification record is mandatory before selection.
`MINDCLADE_DISABLE_TILELANG=1` forces the PyTorch fallback without rebuilding.

Connected-hardware qualification must compile the exact source, inspect codegen,
run sanitizers and adversarial parity, then benchmark against the defined
baseline. Until that evidence is checked in, the candidates remain unavailable
to production dispatch by design.
