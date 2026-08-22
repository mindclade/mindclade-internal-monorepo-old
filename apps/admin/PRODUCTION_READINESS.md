# Apps / Admin Production Readiness

The fail-closed application source, strict type gate, unit suite, approval
primitives, and optimized Next build are implemented. Administrative API
mutation paths remain disabled pending independent security qualification.

## Required evidence

- [x] Boundary and ownership documented
- [x] Browser request deadlines, cancellation, response-size limits, and mutation single-flight behavior tested locally
- [x] Session validation and fail-closed anonymous/error states tested locally
- [x] Nonce-bound CSP, no-store response policy, and mutation preconditions tested locally
- [x] Automated WCAG 2.1 AA, keyboard, touch-target, mobile, and reflow browser checks pass locally
- [x] Hermetic standalone build and deterministic Bazel release archive implemented
- [ ] Readiness, liveness, drain, cancellation, and shutdown tested
- [ ] Security, tenant isolation, and audit behavior tested
- [x] Source operations and rollback runbook linked
- [ ] SLOs, dashboards, and alerts connected
- [ ] Build provenance, SBOM, signatures, and rollback evidence attached
- [x] Explicit limitations recorded

## Current limitations

- Administrative routes are read-only contract boundaries until the reviewed
  admin OpenAPI, elevated session, policy decision, and immutable audit APIs land.
- Approval components collect confirmation and reason but cannot authorize;
  server-side policy remains authoritative.
- CSP, session, and representative accessibility mechanisms are source-tested;
  tenant isolation, emergency-access paging/review, ingress enforcement, audit
  durability, manual assistive technology review, signed build provenance,
  canary, and rollback still require deployed qualification.

Evidence and the connected qualification boundary are recorded in
[`docs/qualification/typescript-applications.md`](../../docs/qualification/typescript-applications.md).
