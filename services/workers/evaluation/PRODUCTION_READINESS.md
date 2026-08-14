# Evaluation worker production readiness

**Current status:** not promoted; scaffold only.

- [ ] Real adapter implementation composes the owning domain engine.
- [ ] Ticket/scope/budget/fencing verification passes.
- [ ] Cancellation, deadline, retry, idempotency, and drain pass.
- [ ] Output artifact/status commit is atomic and rejects stale attempts.
- [ ] Determinism/provenance contract is recorded and tested.
- [ ] Resource limits and failure diagnostics are qualified.
- [ ] Connected provider and end-to-end pipeline tests pass.
- [ ] Bazel/Nix image, SBOM, provenance, security, rollback, and runbook evidence pass.

No deployment promotion is allowed until the checklist links concrete evidence.
