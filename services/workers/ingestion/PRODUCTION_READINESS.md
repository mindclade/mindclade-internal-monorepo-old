# Ingestion worker production readiness

**Current status:** adapter implemented; provider composition not promoted.

- [x] Adapter implementation composes an injected owning-domain engine.
- [x] Shared Rust worker runtime enforces ticket, scope, budget, and fencing contracts.
- [x] Adapter lifecycle and bounded execution behavior have component coverage.
- [ ] Connected cancellation, deadline, retry, idempotency, and drain pass with a real provider.
- [ ] Output artifact/status commit is atomic and rejects stale attempts.
- [ ] Determinism/provenance contract is recorded and tested.
- [ ] Resource limits and failure diagnostics are qualified.
- [ ] Connected provider and end-to-end pipeline tests pass.
- [ ] Bazel/Nix image, SBOM, provenance, security, rollback, and runbook evidence pass.

No deployment promotion is allowed until the checklist links concrete evidence.
