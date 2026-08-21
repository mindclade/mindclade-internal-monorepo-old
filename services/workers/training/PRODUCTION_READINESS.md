# Training worker production readiness

**Current status:** adapter implemented; deployment remains disabled.

- [x] Adapter composes the owning engine through the unified stage contract.
- [x] Local stage kind, deadline, cancellation, concurrency, and drain behavior are tested.
- [x] Rust remains authoritative for ticket, artifact scope, budget, and fencing verification.
- [ ] Owning scientific engine has connected correctness and determinism evidence.
- [ ] Rust-to-Python process/bulk-buffer integration and failure injection pass.
- [ ] Atomic output/status commit rejects stale attempts in an end-to-end test.
- [ ] Resource limits, performance envelope, and failure diagnostics are qualified.
- [ ] Image, SBOM, provenance, security, rollback, and runbook evidence pass.

Kubernetes activation must remain suspended until every unchecked item links concrete evidence.
