# Model worker production readiness

**Current status:** implemented adapter core; not production-qualified.

- [x] Python owns final tensor-aware batch planning.
- [x] Adapter enforces bounded request count and estimated GPU batch bytes.
- [x] Duplicate/omitted request scheduling is rejected.
- [x] Explicit readiness/drain/stop lifecycle exists.
- [x] Deterministic fake-engine/planner tests cover the adapter contract.
- [ ] Rust-host control and bulk IPC is exercised end-to-end with the real process.
- [ ] Ticket/artifact-scope/deadline/fencing context is mapped from canonical runtime protobufs.
- [ ] Real PyTorch model-engine cancellation and timeout behavior is qualified.
- [ ] GPU memory estimates are calibrated against supported model/runtime bundles.
- [ ] Numerical, deterministic-seed, model-loading, and checkpoint compatibility tests pass.
- [ ] Load, restart, drain, preemption, and failure-injection evidence passes.
- [ ] Bazel/Nix image, SBOM, provenance, signing, security, SLO, rollback, and runbook evidence passes.

No deployment promotion is allowed until unchecked items link concrete evidence.
