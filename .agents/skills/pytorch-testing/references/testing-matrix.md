# PyTorch testing matrix

## Functional and tensor contract

- expected nested input and output structure;
- shapes at ordinary and boundary sizes;
- dtype and device propagation;
- intentional broadcasting and reductions;
- finite outputs and meaningful value ranges;
- non-contiguous inputs when supported.

## Module behavior

- parameter and buffer registration;
- train and eval behavior;
- dropout, normalization, and running-state semantics;
- batch size one;
- parameter sharing;
- `state_dict` keys and strict loading.

## Gradients

- loss depends on every expected trainable parameter;
- gradients are non-`None` and finite;
- frozen parameters remain unchanged;
- custom functions pass numerical gradient checks;
- mixed precision unscaling and clipping order is tested when customized.

## Optimizer and training smoke

A single deterministic step should:

1. clear gradients;
2. run forward and loss;
3. backward;
4. update expected parameters;
5. leave excluded parameters unchanged;
6. produce finite metrics.

A tiny-batch overfit test is valuable for full training plumbing, but keep it short and use a clear convergence threshold rather than expecting an exact loss.

## Devices and dtypes

Always provide a fast CPU case unless the component is explicitly accelerator-only. Add conditional cases for supported CUDA, MPS, XPU, or other backends. Test float32 first, then the reduced-precision modes the product claims to support.

## Reproducibility

Test repeatability only within a clearly defined environment. `torch.use_deterministic_algorithms(True)` can expose nondeterministic operations, but deterministic execution can be slower and is not guaranteed across releases or platforms.

## Serialization

Create fresh objects for round-trip tests. Prefer `state_dict`, use strict loading by default, and compare outputs after restoration. For checkpoint migrations, test old keys or versions explicitly.

## Compile and export

When supported, compare eager outputs and gradients against compiled execution across representative shapes. For export, load the saved artifact in a fresh path and test all declared dynamic-shape boundaries.

Official references:

- torch.testing: https://docs.pytorch.org/docs/stable/testing.html
- Gradcheck: https://docs.pytorch.org/docs/stable/notes/gradcheck.html
- Reproducibility: https://docs.pytorch.org/docs/stable/notes/randomness.html
- Serialization: https://docs.pytorch.org/docs/stable/notes/serialization.html
