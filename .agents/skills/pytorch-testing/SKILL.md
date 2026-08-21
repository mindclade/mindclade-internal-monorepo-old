---
name: pytorch-testing
description: Design and implement PyTorch tests for tensor contracts, numerical correctness, gradients, train and eval behavior, device and dtype support, serialization, determinism, data pipelines, compilation, export, and regressions. Use when adding coverage, reproducing a PyTorch defect, hardening a model or trainer, or reviewing test quality. Do not use to replace running the real integration path.
license: MIT
compatibility: Designed for Codex and other Agent Skills-compatible clients. Project commands require Python and the repository's installed PyTorch version.
metadata:
  version: "1.0.0"
  domain: "pytorch"
---
# Objective

Create fast tests that fail for the real defect, state numerical and platform assumptions explicitly, and cover the contract from tensor boundary through serialization or integration as needed.

# Workflow

1. Read the implementation, public contract, existing test utilities, supported Python and PyTorch versions, device matrix, and CI resource limits.
2. Reproduce the defect or missing guarantee with the smallest deterministic test before editing production code whenever possible.
3. Build coverage in layers: pure tensor or functional tests, module contract tests, backward or optimizer-step tests, serialization tests, data-pipeline tests, then the smallest integration smoke path.
4. Use representative edge cases: batch size one, smallest valid dimensions, variable shapes, empty values when supported, train and eval modes, and non-contiguous input when the contract permits it.
5. Compare tensors with `torch.testing.assert_close` and explicit `rtol` and `atol` chosen from the operation, dtype, device, and reduction scale. Avoid blanket exact equality for floating-point results.
6. Test gradients. For ordinary modules, verify expected gradients are present and finite. For custom differentiable functions, use `gradcheck` or `gradgradcheck` with small double-precision inputs when applicable.
7. Test state behavior. Confirm parameters or buffers change only when intended, evaluation does not accumulate gradients, and a `state_dict` round trip preserves outputs.
8. Parameterize CPU and supported accelerators without making unavailable hardware fail local development. Mark genuinely slow or hardware-specific tests narrowly.
9. Add eager-versus-compiled or eager-versus-exported parity tests only when those paths are part of the supported contract.
10. Run the focused test first, then the relevant suite. Report commands, seeds, devices, dtypes, tolerances, and skipped coverage.

# Testing rules

- A regression test must fail on the unfixed code for the intended reason.
- Do not use a seed as a substitute for numerical tolerance or contract assertions.
- Do not assert exact bitwise equality across different devices, PyTorch releases, or parallel algorithms unless the contract explicitly promises it.
- Keep unit tests free of network access, large model downloads, and uncontrolled external data.
- Avoid broad exception assertions. Match the error type and an actionable stable fragment when error behavior is part of the contract.
- Do not skip an entire test module because one device is unavailable. Parameterize and skip the unavailable case.
- Keep performance tests separate from correctness tests unless a generous regression threshold is essential and the runner is controlled.

# Definition of done

- The new or corrected behavior is protected by a focused test.
- Numerical tolerances and supported devices and dtypes are explicit.
- Gradient and state behavior are covered where learning depends on them.
- Serialization or resume behavior is tested in fresh objects.
- The focused test and relevant suite pass, with pre-existing failures separated.

Read [the testing matrix](references/testing-matrix.md) and adapt [the example test module](assets/pytest-pytorch-example.py) rather than copying it blindly.
