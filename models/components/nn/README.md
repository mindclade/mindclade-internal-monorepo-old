# Models / Components / Neural network primitives

- **Status:** Eager PyTorch SwiGLU, feed-forward, and residual primitives are package-tested.
- **Primary implementation ownership:** Python/PyTorch

## Implemented contract

`SwiGLU` is the eager semantic reference: it computes SiLU in float32 before
multiplying and casting to the input dtype. Qualified accelerator providers
must demonstrate parity with this equation.
`SwiGLUFeedForward` maps `[..., hidden_size]` through registered `gate_proj`,
`up_proj`, and `down_proj` layers while preserving all leading dimensions.
`ResidualAdd` performs strict, non-broadcasting `residual + scale * update`.

All inputs are nonempty dense strided floating tensors. Operands and parameters
must already share shape where applicable, dtype, and device; the caller owns
placement. These primitives contain no stochastic train/eval behavior.

## Boundary

Reusable implementation belongs in this owning package. Deployable entry points,
provider construction, health/drain wiring, and deployment evidence belong under
`services/`. Cross-language data exchanged outside a process uses versioned
contracts under `protocols/` rather than language-private structures.

This package must not become a `common`, `shared`, `helpers`, or `utils` dumping
ground. It may depend only in the direction documented by
`docs/architecture/dependency-rules.md` and the accepted ADRs.

## Remaining scaffolds and qualification

Dropout, stochastic depth, and parametrization remain explicit scaffold files.
This package does not choose a qualified accelerator kernel or claim hardware
performance. Model-owned adapters may replace the reference activation only
through the repository's qualification-gated kernel boundary.

Focused validation:

```text
bazel test //models/components/nn/tests:test_nn
```
