# Runtime gateway production readiness

**Maturity:** `implemented` core, **not `qualified` and not `production`**.

Source behavior and local test contracts exist. Promotion remains forbidden
until the connected/runtime evidence below is present.

## Implemented evidence

- [x] Model-independent admission/routing/authority core has real source behavior.
- [x] Integration and shutdown test sources exist.
- [x] Queue/resource limits and fail-closed transitions are represented in types/state.
- [x] Canonical execution/grant/route/revocation contracts are shared with Go/Rust fixtures.

## Required qualification evidence

- [ ] `cargo test --workspace` and service-target tests pass under the pinned toolchain.
- [ ] Clippy and Rust workspace/layer policies pass.
- [ ] Cross-language golden vectors pass with compiled Rust consumers.
- [ ] Tonic/Tokio transport integration is exercised under cancellation and backpressure.
- [ ] Fuzz/concurrency tests cover ticket, route, stream, and framing inputs.
- [ ] Revocation, stale authority, replay, fencing, outage, and drain fault tests pass.
- [ ] p50/p95/p99 latency, queue, RSS, FD, cancellation, shutdown, and recovery budgets pass.
- [ ] Security/tenant-isolation review is complete.
- [ ] Bazel/Nix hermetic image, SBOM, provenance, signatures, smoke, and rollback pass.

## Evidence index

| Evidence | Current state | Location |
|---|---|---|
| Source/core contracts | implemented | `src/` |
| Unit/component test source | implemented, not executed here | `tests/` |
| Compiled Rust qualification | pending | Rust connected CI |
| Cross-language compiled conformance | pending | `tests/integration/cross_language/` |
| Performance qualification | pending | runtime qualification lane |
| Security review | pending | security evidence bundle |
| Build/provenance/SBOM | pending | Bazel/Nix release lane |
| Deployment smoke/rollback | pending | deployment qualification lane |
