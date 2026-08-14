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

- [ ] `cargo test --workspace` and host-target tests pass under the pinned toolchain.
- [ ] Clippy, Miri/sanitizers for allowed unsafe leaves, and Rust workspace policies pass.
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
| Unit/component test source | implemented, not executed here | `tests/` |
| Compiled Rust qualification | pending | Rust connected CI |
| Process/IPC fault qualification | pending | runtime qualification lane |
| Performance/resource qualification | pending | runtime qualification lane |
| Security review | pending | security evidence bundle |
| Build/provenance/SBOM | pending | Bazel/Nix release lane |
| Deployment smoke/rollback | pending | deployment qualification lane |
