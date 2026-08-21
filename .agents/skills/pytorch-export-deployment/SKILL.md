---
name: pytorch-export-deployment
description: Export and validate PyTorch models for deployment using torch.export, the torch.export-based ONNX exporter, or a repository-required runtime. Use for graph capture, dynamic shapes, operator compatibility, serialization, artifact manifests, eager-versus-runtime parity, and export failures. Do not use when ordinary state_dict checkpointing is the only requirement.
license: MIT
compatibility: Designed for Codex and other Agent Skills-compatible clients. Project commands require Python and the repository's installed PyTorch version.
metadata:
  version: "1.0.0"
  domain: "pytorch"
---
# Objective

Produce a deployment artifact with an explicit input contract, verified dynamic-shape range, reproducible metadata, and parity against eager PyTorch in the actual target runtime when available.

# Workflow

1. Identify the real consumer: PyTorch runtime, AOTInductor, ONNX Runtime, TensorRT, mobile, edge, or another system. Record supported operators, opset or archive requirements, precision, device, latency constraints, and dynamic dimensions.
2. Detect the installed PyTorch version and inspect the repository's established export path. Do not migrate formats merely because a newer API exists unless the target supports it.
3. Freeze the inference contract: model configuration, preprocessing, input names and nested structure, shapes and allowed dynamic ranges, dtypes, devices, output structure, and postprocessing.
4. Create an eager reference in `eval()` mode and use `torch.inference_mode()` for parity inputs when compatible. Keep representative and boundary input fixtures small and deterministic.
5. Prefer `torch.export.export` for a PyTorch exported program when it matches the target. Specify dynamic shapes deliberately; the default specializes unspecified dimensions.
6. For ONNX on supported versions, prefer the `torch.export`-based exporter with `dynamo=True`. Use `dynamic_shapes` with that path and record the chosen opset and external-data behavior.
7. Refactor data-dependent Python control flow, unsupported container behavior, or custom operators into exportable public constructs when possible. Do not change model semantics merely to silence the exporter.
8. Save the artifact through the format's supported API and load it in a fresh process or runtime. Validate ordinary and boundary shapes against eager outputs with explicit tolerances.
9. Test failure outside the declared dynamic range when the runtime enforces constraints. Verify output names, order, dtypes, and shapes as seen by the consumer.
10. Write an artifact manifest containing model and code identity, PyTorch and exporter versions, opset or archive version, precision, input contract, dynamic ranges, checksums, and validation results.
11. Keep a regression test for export and parity. Report unsupported operations, fallback behavior, runtime-specific caveats, and any untested target hardware.

# Export and security rules

- Do not infer dynamic dimensions solely from one example. Declare the supported range and test its boundaries.
- Do not use legacy tracing as a silent fallback when it changes data-dependent behavior. Explain any required legacy path.
- Keep preprocessing and postprocessing in the contract; exporting only the core module can otherwise produce a technically valid but unusable artifact.
- Prefer saving model data through `state_dict` or the export format's supported save API. Do not load untrusted pickle-capable checkpoints with `weights_only=False`.
- Treat `weights_only=True` as a narrower loading surface, not a complete defense against denial of service or malformed tensor data.
- Do not claim target-runtime parity until the artifact is executed in that runtime or explicitly state that validation is limited to export-time checks.

# Definition of done

- The consumer and input contract are documented.
- Export succeeds through the repository-supported path.
- The artifact loads independently.
- Eager and target outputs agree across representative and boundary shapes within explicit tolerances.
- Dynamic dimensions and unsupported ranges are stated.
- The artifact manifest records versions, precision, checksums, and validation commands.

Read [the export checklist](references/export-checklist.md) before choosing dynamic-shape or ONNX options.
