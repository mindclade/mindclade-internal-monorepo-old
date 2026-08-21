# Models / Reference

- **Status:** The reference affine module is implemented and package-tested. Other reference
  files remain scaffolds. This is not a scientific model or a production-readiness claim.
- **Primary implementation ownership:** Python/PyTorch

## Implemented contract

`ReferenceAffine` is the deterministic operation `reference.affine.v1`:

```text
output = (input * scale) + bias
```

Its only state is two trainable, scalar `torch.float32` parameters named exactly `scale` and
`bias`. The default values (`2.0` and `0.5`) match the reference worker fixture. Input must be a
nonempty, finite, dense strided `torch.float32` tensor on the same device as the parameters.
Output has the same shape, dtype, and device. Noncontiguous inputs and a batch size of one are
supported.

The caller owns placement. Constructing the module puts its parameters on PyTorch's default CPU
device; callers may move the module explicitly. Forward execution never casts or moves inputs or
parameters. Train and evaluation modes have identical deterministic behavior because this
reference operation has no stochastic or mode-dependent layers.

`ReferenceAffineConfig` is frozen and validates finite float32-range initialization values, the
canonical operation and dtype, and a positive input-element budget. The default budget is
16,777,216 elements. Inputs exceeding the configured budget fail before arithmetic.

## State and serialization

Normal PyTorch checkpoints use strict `state_dict` loading with exactly `scale` and `bias`.
`save_reference_affine` and `load_reference_affine` use only the non-executable safetensors
format. The loader accepts a bounded regular `.safetensors` file, rejects symlinks, enforces the
exact keys, scalar shapes, and float32 dtype, and returns a fresh module. The save helper writes
through a same-directory temporary file, atomically replaces the destination, and syncs the
containing directory. Loading reads bounded bytes from one `O_NOFOLLOW` file descriptor before
parsing, so a path cannot be swapped to a symlink between validation and safetensors decoding.
Non-finite parameter values are rejected on both serialization and loading.

These helpers protect the tensor-format and local-file boundary; they do not verify a signed
release manifest, artifact digest, tenant authorization, or provenance. Those responsibilities
remain with the model-bundle and serving layers.

## Limits and non-responsibilities

This module exists to exercise model, gradient, checkpoint, and runtime-contract integration. It
does not make predictions with scientific meaning, select a device, implement distributed
training, provide cancellation, own serving lifecycle behavior, or establish deployment
qualification. The remaining `tiny_*` reference-family paths are still scaffolds.

Focused validation is owned by:

```text
bazel test //models/reference/tests:test_reference
```

Any broader readiness or maturity change requires the repository qualification evidence and
metadata described in the root operating guide.
