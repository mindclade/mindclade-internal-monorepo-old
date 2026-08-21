# FP8 operation contract

- **Implemented:** E4M3FN/E5M2 saturation casting, explicit scaling, scaled GEMM, grouped GEMM, and linear references.
- **TileLang candidate:** Qualification-gated pipelined scaled GEMM.

Inputs are divided by an explicit positive finite scale, saturated to the chosen
format, and cast deterministically. GEMM dequantizes with runtime scales, uses
FP32 accumulation, applies an optional `none`/`relu`/`silu` epilogue, then casts
to the requested output dtype. Scale tensors are not baked into compiled source.

Validation rejects non-floating inputs, invalid scales, incompatible matrix or
expert dimensions, and unsupported formats. FP8 tolerances are fixed by format;
each target must demonstrate its own numerical and performance envelope.
