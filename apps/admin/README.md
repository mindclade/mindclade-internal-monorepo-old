# Mindclade Governance

- **Status:** Implemented fail-closed application surface; administrative API integration is not yet production-qualified.
- **Primary implementation ownership:** TypeScript

The restricted control surface for identity, tenancy, quotas, independent gate
approval, releases, sensitive model-weight access, immutable audit, and
break-glass operations. High-impact components require an operational reason,
explicit confirmation phrase, idempotency identity, and elevated approval.

## Boundary

Reusable implementation belongs in this owning package. Deployable entry points,
provider construction, health/drain wiring, and deployment evidence belong under
`services/`. Cross-language data exchanged outside a process uses versioned
contracts under `protocols/` rather than language-private structures.

This package must not become a `common`, `shared`, `helpers`, or `utils` dumping
ground. It may depend only in the direction documented by
`docs/architecture/dependency-rules.md` and the accepted ADRs.

The current routes deliberately remain read-only until the reviewed
administrative HTTP contract is configured. The UI never infers privilege from
presentation state; the server remains authoritative for policy and audit.
`NEXT_PUBLIC_ENVIRONMENT` labels the target environment and optional telemetry
uses `NEXT_PUBLIC_TELEMETRY_ENDPOINT` with sensitive-field redaction.

`pnpm --filter @mindclade/apps-admin build` produces the optimized Next artifact.
Production promotion still requires elevated-session integration, tenant and
policy tests, CSP/identity qualification, immutable audit verification, signed
build provenance, and a tested deployment rollback.
