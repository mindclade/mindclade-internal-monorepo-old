# Artifact proxy production readiness

**Current status:** core implemented; composition binary and deployment are not promoted.

## Required implementation evidence

- [x] Core source modules contain real provider-neutral behavior.
- [x] Grant, range, publication/read, cache, and provider component tests pass.
- [x] Core buffers, ranges, cache entries, and provider operations have explicit bounds.
- [ ] Composed listener liveness, readiness, drain, cancellation, deadline, and shutdown pass.
- [ ] Fencing, replay, stale authority, and revocation behavior pass fault tests.
- [ ] Security and tenant-isolation review is complete.
- [ ] Performance, peak-memory, file-descriptor, queue, and recovery budgets pass.
- [ ] Bazel/Nix hermetic build, SBOM, provenance, image signing, and release evidence pass.
- [ ] Runbooks and dashboards/alerts are linked below.

## Evidence index

| Evidence | Status | Location |
|---|---|---|
| Unit/component tests | present | `services/artifact_proxy/tests/` |
| Integration/failure tests | partial | `services/artifact_proxy/tests/integration.rs`, `provider.rs` |
| Performance qualification | missing | — |
| Security review | missing | — |
| Build/provenance/SBOM | missing | — |
| Deployment smoke/rollback | missing | — |

Promotion is forbidden while any mandatory evidence remains missing.
