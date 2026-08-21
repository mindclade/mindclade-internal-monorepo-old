# Kernel provider API

- **Status:** Implemented contract; individual accelerator implementations remain qualification-gated.
- **Owner:** `biology-ml`

`KernelRequest` is the canonical dispatch key. It contains the operation,
ordered tensor specifications, target, architecture, semantic attributes, and
gradient/determinism requirements. Canonical JSON and SHA-256 digests prevent a
qualification for one signature from authorizing another.

`ImplementationIdentity` separately binds provider, source, compiler version,
and schedule. `DeviceCapabilities` records runtime facts rather than inferring
support from a marketing model name.

## Rules

- Shapes, dtypes, layouts, devices, and semantic attributes are explicit.
- Unknown attributes or layouts are rejected by operation validation.
- Provider eligibility is pure and returns a stable rejection reason.
- PyTorch is the semantic reference; accelerator presence is not qualification.
- Errors use stable `KernelErrorCode` values and redact runtime internals.

The API owns in-process Python contracts only. Cross-process schemas belong in
`protocols/`, service lifecycle belongs in `services/`, and model selection
policy belongs in `models/`.
