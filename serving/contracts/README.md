# Serving contracts

**Status:** implemented reusable Python inference contracts.

This package is the Python-side contract between process/runtime admission and
model/tensor execution. It intentionally does not implement network serving or
Rust node authority.

## Implemented contracts

- `InferenceRequest` and bounded immutable input descriptors;
- `CompatibilityKey`, `BatchPlan`, and the Python `BatchPlanner` protocol;
- validated `InferenceResult`;
- content-bound `ModelBundle` and deterministic `RuntimeManifest` fingerprinting;
- validation helpers and package-local tests.

The critical ownership rule is that Rust may perform coarse admission and
compatibility grouping, while Python makes the final tensor-aware batching
decision. This prevents model-specific shape/KV/MSA/atom/diffusion semantics from
being reimplemented in the Rust runtime.

Cross-process/wire contracts remain canonical under `protocols/`; these Python
records adapt that wire contract to numerical execution.
