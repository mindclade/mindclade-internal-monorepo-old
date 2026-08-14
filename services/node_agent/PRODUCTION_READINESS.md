# Node agent production readiness

**Current status:** not promoted. The archive contains a target-state service
scaffold and boundary documentation, not a qualified service binary.

## Required implementation evidence

- [ ] All source modules contain real behavior rather than scaffold constants.
- [ ] Protocol compatibility and cross-language golden vectors pass.
- [ ] Every queue/task/process/resource allocation is bounded and owned.
- [ ] Liveness, readiness, drain, cancellation, deadline, and shutdown tests pass.
- [ ] Fencing, replay, stale authority, and revocation behavior pass fault tests.
- [ ] Security and tenant-isolation review is complete.
- [ ] Performance, peak-memory, file-descriptor, queue, and recovery budgets pass.
- [ ] Bazel/Nix hermetic build, SBOM, provenance, image signing, and release evidence pass.
- [ ] Runbooks and dashboards/alerts are linked below.

## Evidence index

| Evidence | Status | Location |
|---|---|---|
| Unit/component tests | missing | — |
| Integration/failure tests | missing | — |
| Performance qualification | missing | — |
| Security review | missing | — |
| Build/provenance/SBOM | missing | — |
| Deployment smoke/rollback | missing | — |

Promotion is forbidden while any mandatory evidence remains missing.
