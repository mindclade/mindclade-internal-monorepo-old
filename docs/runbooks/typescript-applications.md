# Runbook: TypeScript browser applications

## Status and authority

This is source operational guidance for Mindclade Command (`apps/console`) and
Mindclade Governance (`apps/admin`). It is not evidence that either application
is deployed. This repository owns source and release-artifact definitions;
`infrastructure-live` owns GKE, load balancing, Cloud DNS, and certificates;
GitOps owns in-cluster gateways and deployed immutable digests.

## Runtime contract

- Command uses `NEXT_PUBLIC_API_BASE_URL`, or its own origin for a BFF, and
  optionally `NEXT_PUBLIC_TELEMETRY_ENDPOINT`.
- Governance deliberately uses same-origin identity and administrative routes;
  it accepts only the optional telemetry endpoint.
- Both applications call `GET /auth/session` with cookies. `401` is anonymous.
  A successful response must contain a non-empty principal ID, display name and
  organization ID, string scopes, a parseable expiry, and `standard` or
  `elevated` assurance. Expired, malformed, unavailable, and oversized responses
  fail closed.
- Browser state is never authorization. Owning services enforce tenant,
  assurance, mutation, idempotency, and audit policy.

Production endpoints must be HTTPS and must not contain credentials. HTML is
request-rendered with a fresh script/style nonce; CSP allows only the configured
API and telemetry origins and rejects inline script attributes. Governance
responses additionally use `Cache-Control: no-store` and
`Referrer-Policy: no-referrer`.

## Build and inspect

Use the pinned Bazel/Nix environment:

```bash
tools/dev/nixw develop .#ci-bazel --command \
  tools/dev/bazelw test //apps/console:unit_tests //apps/admin:unit_tests --config=ci
tools/dev/nixw develop .#ci-bazel --command \
  tools/dev/bazelw build //apps/console:release_archive //apps/admin:release_archive --config=ci
```

Run the representative browser-accessibility matrix against optimized builds:

```bash
pnpm build
pnpm exec playwright install chromium firefox webkit
pnpm test:accessibility
```

On macOS, the Chromium projects use the installed stable Chrome channel;
Firefox and WebKit remain Playwright-pinned. Linux CI installs all three pinned
engines plus their system dependencies before running the same suite.

The archives contain the Next standalone server, public files, and static
assets with portable timestamps and no build stamping. A release lane must add
an SBOM, provenance attestation, signature, immutable image digest, and
environment approval; do not publish the source archive directly.

## Deployment preflight

The target design is a private GKE Standard application workload on a
purpose-specific CPU pool, not a GPU serving or CI pool. Use Workload Identity
with no Google Cloud permissions unless a documented runtime dependency needs
them. Ingress, egress, firewall, DNS, certificate, and gateway changes require
their owning infrastructure review and protected approval.

Use an explicit production service name under `mindclade.ai` and explicit
staging/development subdomains; do not add a broad wildcard API record. DNS and
certificate cutover must preserve the registrar/Cloud DNS authority split,
DNSSEC rollback, and the existing mail posture. These are proposed deployment
constraints until connected infrastructure evidence is attached.

Before traffic:

1. Pin the image by digest and verify its signature, SBOM, provenance, and
   admission result.
2. Verify root and representative route responses, CSP, HSTS, no-sniff,
   frame-denial, referrer, and cache headers through the actual load balancer.
3. Test anonymous, expired, malformed, wrong-tenant, insufficient-scope, and
   elevated-session cases against a non-production IdP and API.
4. For Governance, prove duplicate submission is idempotent and that rejected,
   timed-out, or redirected mutations create no privileged state.
5. Canary the immutable digest, observe request/error/latency and session/API
   failure signals, then advance traffic only within the approved error budget.

## Incident response and rollback

If identity or policy state is unavailable, keep the UI fail closed; do not add
a bypass or fabricate a principal. If headers are missing, the wrong environment
is shown, mutation behavior is ambiguous, or error rate/latency breaches the
approved objective, stop promotion and route back to the last known-good signed
digest. Preserve the failed digest, request IDs, header captures, audit outcome,
traffic split, and rollback reason. Governance stays disabled until immutable
audit and policy owners confirm the incident boundary.

## Exit criteria

Source qualification is complete when generation, type, unit, boundary,
standalone build, browser accessibility automation, and archive targets pass.
Production qualification additionally requires connected IdP/API negative
tests, ingress evidence, manual assistive-technology review and browser checks
through the production edge, SLO dashboards/alerts, signed release evidence,
canary, and a recorded rollback exercise.
