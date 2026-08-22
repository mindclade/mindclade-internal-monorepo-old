# Fused operation contracts

- **Implemented:** SwiGLU and incoming/outgoing Pairformer triangle multiplication references and TileLang candidates.

SwiGLU computes `silu(gate) * up` with FP32-or-better nonlinear evaluation.
Triangle multiplication consumes `[B,N,N,C]` left/right
tensors and a `[B,N]` mask. It masks the reduction axis and zeroes invalid output
pairs; incoming and outgoing contractions have separate operation identities.

Other named files in this package remain model-facing extension points and are
not registered kernel capabilities by this implementation.
