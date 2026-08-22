# Models / Components / Normalization

- **Status:** Eager PyTorch RMSNorm and LayerNorm implementations are package-tested.
- **Primary implementation ownership:** Python/PyTorch

## Implemented contract

`RMSNorm` and `LayerNorm` accept nonempty dense strided floating tensors whose
trailing dimensions exactly equal `normalized_shape`. They preserve shape,
dtype, and device and never move or cast caller input implicitly. Affine state
must already share input dtype and device.

`RMSNorm` computes the mean square in float32 for float16, bfloat16, and
float32 inputs, and retains float64 reduction for float64 input. `LayerNorm`
uses the installed PyTorch functional implementation. State keys are `weight`
and optional `bias`; parameter-free configurations have empty state dicts.

NaN and infinity follow PyTorch propagation semantics. Forward does not scan
tensor values or introduce a device synchronization.

## Boundary

Reusable implementation belongs in this owning package. Deployable entry points,
provider construction, health/drain wiring, and deployment evidence belong under
`services/`. Cross-language data exchanged outside a process uses versioned
contracts under `protocols/` rather than language-private structures.

This package must not become a `common`, `shared`, `helpers`, or `utils` dumping
ground. It may depend only in the direction documented by
`docs/architecture/dependency-rules.md` and the accepted ADRs.

## Limits and qualification

Construction bounds normalized rank and parameter count. Input rank is bounded
at sixteen. These modules do not select devices, define mixed-precision policy,
or claim accelerator-specific numerical or performance qualification.

Focused validation:

```text
bazel test //models/components/normalization/tests:test_normalization
```
