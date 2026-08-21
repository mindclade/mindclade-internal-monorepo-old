# Mindclade Command

- **Status:** Implemented application surface; authentication, live APIs, and deployment are not yet production-qualified.
- **Primary implementation ownership:** TypeScript

The product-facing operational cockpit for AI research and model systems. It
turns runs, datasets, models, artifacts, and independent evaluations into one
evidence-oriented workspace. Routes backed by the reviewed public contract use
the TypeScript SDK directly. Reserved platform capabilities fail visibly at a
contract boundary instead of displaying fabricated operational state.

## Boundary

Reusable implementation belongs in this owning package. Deployable entry points,
provider construction, health/drain wiring, and deployment evidence belong under
`services/`. Cross-language data exchanged outside a process uses versioned
contracts under `protocols/` rather than language-private structures.

This package must not become a `common`, `shared`, `helpers`, or `utils` dumping
ground. It may depend only in the direction documented by
`docs/architecture/dependency-rules.md` and the accepted ADRs.

## Runtime contract

Set `NEXT_PUBLIC_API_BASE_URL` for a separate API origin; otherwise the browser
uses its own origin and sends BFF session cookies. Production endpoint policy
rejects credentials and non-HTTPS non-loopback origins. The shell verifies
`/auth/session` before displaying a principal and fails closed on an expired,
malformed, unavailable, or anonymous session. It does not infer authorization.

Optional telemetry requires `NEXT_PUBLIC_TELEMETRY_ENDPOINT`; no telemetry is
sent when it is absent. Requests cancel on route/component disposal, share one
overall retry deadline, and bound JSON, event-stream, parser, and telemetry
queue memory. Shared CSP and response headers are generated from the exact API
and telemetry origins; HTML is request-rendered with a fresh script/style nonce.

`pnpm --filter @mindclade/apps-console build` produces the optimized Next
artifact. `bazel build //apps/console:release_archive --config=ci` produces the
deterministic standalone release input; provenance, SBOM, signing, image
assembly, and promotion remain protected release-lane responsibilities. The
interface includes loading, empty, error, reduced-motion, keyboard-focus, and
narrow-viewport states. No model payloads, credentials, or policy decisions are
stored in the application. API compatibility is governed separately.

See the [browser application runbook](../../docs/runbooks/typescript-applications.md)
and [source qualification record](../../docs/qualification/typescript-applications.md).
