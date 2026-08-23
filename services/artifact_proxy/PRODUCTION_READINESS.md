# Artifact proxy production readiness

**Current status:** core implemented; composition binary and deployment are not promoted.

## Required implementation evidence

- [x] Core source modules contain real provider-neutral behavior.
- [x] Grant, range, publication/read, cache, and provider component tests pass.
- [x] Core buffers, ranges, cache entries, and provider operations have explicit bounds.
- [ ] Composed listener liveness, readiness, drain, cancellation, deadline, and shutdown pass.
      Partially evidenced and deliberately left unchecked. `tests/composition.rs` drives the
      real composition root over a real socket and proves liveness, readiness (including the
      transition to ready, the drop to 503 when the object store stops answering, and the
      recovery when it returns), drain on termination, and fail-closed startup. Cancellation
      and deadline behavior on a *connected* listener are not covered, because there is no
      byte-plane protocol to connect to. This item stays open until there is.
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
