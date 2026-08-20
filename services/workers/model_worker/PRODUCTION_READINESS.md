# Model worker production readiness

**Current status:** reference execution path qualified on CPU; scientific/GPU deployment remains disabled.

- [x] Python owns final tensor-aware batch planning.
- [x] Adapter enforces bounded request count and estimated GPU batch bytes.
- [x] Duplicate/omitted request scheduling is rejected.
- [x] Explicit readiness/drain/stop lifecycle exists.
- [x] Deterministic fake-engine/planner tests cover the reusable batching contract.
- [x] Generated Python and Rust runtime protobufs share frozen wire vectors.
- [x] Rust-host control and descriptor IPC is exercised end-to-end with the supervised executable process.
- [x] Ticket/artifact-scope/deadline/fencing context is mapped from canonical runtime protobufs.
- [x] PyTorch reference execution, cancellation, framing bounds, and digest-tamper failures are tested.
- [x] Checkpoint loading is safetensors-only and verifies the signed bundle manifest and members first.
- [ ] GPU memory estimates are calibrated against supported model/runtime bundles.
- [ ] Numerical, deterministic-seed, model-loading, and checkpoint compatibility tests pass.
- [ ] Load, restart, drain, preemption, and failure-injection evidence passes.
- [ ] Bazel/Nix image, SBOM, provenance, signing, security, SLO, rollback, and runbook evidence passes.

Current evidence is `//services/workers/model_worker/tests:test_runtime` and
`//services/runtime_host:execution_transport`. The reference affine bundle is
test/qualification infrastructure, not a production model. No deployment
promotion is allowed until unchecked items link concrete evidence.
