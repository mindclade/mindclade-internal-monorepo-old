# Apps / Admin Production Readiness

The fail-closed application source, strict type gate, unit suite, approval
primitives, and optimized Next build are implemented. Administrative API
mutation paths remain disabled pending independent security qualification.

## Required evidence

- [x] Boundary and ownership documented
- [ ] Resource limits and backpressure qualified
- [ ] Readiness, liveness, drain, cancellation, and shutdown tested
- [ ] Security, tenant isolation, and audit behavior tested
- [ ] SLOs, dashboards, alerts, and runbooks linked
- [ ] Build provenance, SBOM, signatures, and rollback evidence attached
- [x] Explicit limitations recorded

## Current limitations

- Administrative routes are read-only contract boundaries until the reviewed
  admin OpenAPI, elevated session, policy decision, and immutable audit APIs land.
- Approval components collect confirmation and reason but cannot authorize;
  server-side policy remains authoritative.
- Tenant isolation, emergency-access paging/review, CSP, audit durability,
  accessibility automation, signed build provenance, canary, and rollback still
  require deployed qualification.
