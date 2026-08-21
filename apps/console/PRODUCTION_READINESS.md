# Apps / Console Production Readiness

The application source, strict type gate, unit suite, and optimized Next build
are implemented. This checklist intentionally does not claim deployment or live
identity/service qualification.

## Required evidence

- [x] Boundary and ownership documented
- [x] Client timeout, cancellation, stream-size, parser-size, and telemetry queue limits tested locally
- [ ] Readiness, liveness, drain, cancellation, and shutdown tested
- [ ] Security, tenant isolation, and audit behavior tested
- [ ] SLOs, dashboards, alerts, and runbooks linked
- [ ] Build provenance, SBOM, signatures, and rollback evidence attached
- [x] Explicit limitations recorded

## Current limitations

- Authentication, CSP, tenant isolation, accessibility automation, and live API
  failure injection still require a deployed qualification environment.
- Cluster, serving, rollout, checkpoint, kernel, experiment, preprocessing, and
  safety routes are honest contract-boundary states until their owning APIs land.
- There is no signed OCI/web artifact, canary, SLO, alert, or production rollback
  record yet.
