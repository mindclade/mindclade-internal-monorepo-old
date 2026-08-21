# Diffusion operation contracts

- **Implemented:** adaptive modulation/gated residual and safe sparse neighbor-attention references.
- **TileLang candidate:** Qualification-gated fused modulation/gate/residual epilogue.

The epilogue computes
`residual + gate * (normalized * (1 + scale) + shift)` for `[B,T,C]` activations
and `[B,C]` modulation tensors without materialized broadcasting in the TileLang
candidate. Reduced-precision inputs evaluate the arithmetic in FP32.

Sparse neighbor attention accepts `-1` as padding, rejects every other
out-of-bounds index, applies softmax only over valid neighbors, and returns zero
for rows with no valid neighbors. Sampling algorithms and model preconditioning
policy are outside this primitive boundary.
