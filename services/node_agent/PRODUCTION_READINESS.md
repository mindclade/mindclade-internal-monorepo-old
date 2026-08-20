# Node agent production readiness

**Current status:** implemented core, not promoted or production-qualified.

## Required implementation evidence

- [x] Source modules contain real bounded behavior rather than scaffold tests/constants.
- [ ] Protocol compatibility and cross-language golden vectors pass.
- [x] Local queue/cache/process/output/resource allocations have explicit bounds and ownership.
- [x] Local liveness, drain, cancellation, deadline, process reaping, and shutdown tests pass on pinned Rust 1.97.1.
- [ ] Fencing, replay, stale authority, and revocation behavior pass fault tests.
- [ ] Security and tenant-isolation review is complete.
- [ ] Performance, peak-memory, file-descriptor, queue, and recovery budgets pass.
- [ ] Bazel/Nix hermetic build, SBOM, provenance, image signing, and release evidence pass.
- [ ] Runbooks and dashboards/alerts are linked below.

## Evidence index

| Evidence | Status | Location |
|---|---|---|
| Unit/component tests | passing locally | `tests/` |
| Integration/failure tests | local deterministic cases pass; connected injection pending | `tests/` |
| Performance qualification | missing | — |
| Security review | missing | — |
| Build/provenance/SBOM | missing | — |
| Deployment smoke/rollback | missing | — |

Promotion is forbidden while any mandatory evidence remains missing.
