# Runtime host production readiness

**Maturity:** `implemented` core, **not `qualified` and not `production`**.

Source behavior and local test contracts exist. Promotion remains forbidden
until the connected/runtime evidence below is present.

## Implemented evidence

- [x] Node budget, process supervision, slot reservation, IPC, and drain core has real source behavior.
- [x] Integration and shutdown test sources exist.
- [x] Python remains process-isolated; tensor/model semantics are outside Rust authority.
- [x] Control IPC and bulk-data descriptors are separate contracts.

## Required qualification evidence

- [x] `cargo test --workspace --all-targets --all-features --locked` and host tests pass under pinned Rust 1.97.1 (2026-08-20 local evidence).
- [x] Clippy with warnings denied and Rust workspace static policies pass (2026-08-20 local evidence).
- [ ] Miri/sanitizers and Linux failure injection pass for the allowed `ipc_os` unsafe leaf.
- [ ] Worker process crash/restart, claim loss, cancellation, preemption, and drain tests pass.
- [ ] OS-specific shared-memory/memfd/fd bulk adapters are implemented only where benchmarked and qualified.
- [ ] GPU/host/pinned/shared-memory budget and oversubscription stress tests pass.
- [ ] Peak RSS, FD, process count, restart, cancellation, shutdown, and recovery budgets pass.
- [ ] Security/tenant/process-isolation review is complete.
- [ ] Bazel/Nix hermetic image, SBOM, provenance, signatures, smoke, and rollback pass.

## Evidence index

| Evidence | Current state | Location |
|---|---|---|
| Source/core contracts | implemented | `src/` |
| Unit/component tests | passing locally on pinned toolchain | `tests/` |
| Compiled Rust qualification | local Cargo gates pass; connected release lane pending | `tools/qualification/rust/` |
| Process/IPC fault qualification | pending | runtime qualification lane |
| Performance/resource qualification | pending | runtime qualification lane |
| Security review | pending | security evidence bundle |
| Build/provenance/SBOM | pending | Bazel/Nix release lane |
| Deployment smoke/rollback | pending | deployment qualification lane |
