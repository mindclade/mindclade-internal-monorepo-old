# Apps / Console Production Readiness

The application source, strict type gate, unit suite, and optimized Next build
are implemented. This checklist intentionally does not claim deployment or live
identity/service qualification.

## Required evidence

- [x] Boundary and ownership documented
- [x] Client timeout, cancellation, stream-size, parser-size, and telemetry queue limits tested locally
- [x] Session validation and fail-closed anonymous/error states tested locally
- [x] Nonce-bound CSP, browser response headers, and production endpoint policy tested locally
- [x] Hermetic standalone build and deterministic Bazel release archive implemented
- [ ] Readiness, liveness, drain, cancellation, and shutdown tested
- [ ] Security, tenant isolation, and audit behavior tested
- [x] Source operations and rollback runbook linked
- [ ] SLOs, dashboards, and alerts connected
- [ ] Build provenance, SBOM, signatures, and rollback evidence attached
- [x] Explicit limitations recorded

## Current limitations

- Session and CSP mechanisms are source-tested; IdP integration, ingress header
  enforcement, tenant isolation, accessibility automation, and live API failure
  injection still require a deployed qualification environment.
- Cluster, serving, rollout, checkpoint, kernel, experiment, preprocessing, and
  safety routes are honest contract-boundary states until their owning APIs land.
- There is no signed OCI/web artifact, canary, SLO, alert, or production rollback
  record yet.

Evidence and the connected qualification boundary are recorded in
[`docs/qualification/typescript-applications.md`](../../docs/qualification/typescript-applications.md).
