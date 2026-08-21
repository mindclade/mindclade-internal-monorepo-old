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

The shell verifies the same-origin `/auth/session` contract and displays only a
validated, unexpired principal. Administrative request helpers bound deadlines
and decoded response bytes, reject redirects, require an idempotency key and
operational reason for mutations, and parse structured problem responses.
Approval controls additionally require immutable evidence by default, exact
confirmation, mutation authority, and single-flight submission. These browser
controls are defense in depth and never replace server authorization or audit.
HTML is request-rendered with a fresh CSP nonce and no-store policy.

`pnpm --filter @mindclade/apps-admin build` produces the optimized Next artifact.
`bazel build //apps/admin:release_archive --config=ci` produces the deterministic
standalone release input; protected release automation must still attach the
SBOM, provenance, signature, image, and immutable promotion record.
Production promotion still requires elevated-session integration, tenant and
policy tests, ingress CSP/identity qualification, immutable audit verification,
and a tested deployment rollback. See the
[browser application runbook](../../docs/runbooks/typescript-applications.md)
and [source qualification record](../../docs/qualification/typescript-applications.md).
