# PyTorch kernel provider

- **Status:** Implemented semantic reference and fallback provider.
- **Owner:** `biology-ml`

This provider registers independent PyTorch implementations for attention,
scaled FP8 GEMM, SwiGLU, Pairformer triangle multiplication, deterministic MoE
primitives, and diffusion epilogues/neighbor attention. Numerically sensitive
reductions use FP32 for FP8/FP16/BF16 inputs and preserve FP64 for gradient
checking.

Reference functions validate ranks, shapes, mask meaning, index bounds, dtypes,
and empty/all-masked cases. They are intentionally straightforward: they define
semantics and provide a safe fallback, not an accelerator performance baseline.

`register_pytorch_references` installs exactly one reference per operation. A
reference may be used when every accelerator candidate is ineligible,
unqualified, revoked, or disabled by the TileLang kill switch.
