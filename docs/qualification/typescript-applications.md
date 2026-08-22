# TypeScript SDK and browser application source qualification

**Status:** PASS_WITH_DEPLOYMENT_PREFLIGHT
**Evidence level:** source
**Review date:** 2026-08-21

## Scope

- `sdk/typescript`
- `libs/ts`
- `apps/console`
- `apps/admin`
- their Bazel, pnpm, protobuf/OpenAPI generation, and source documentation

The source monorepo owns these implementations and deterministic release
inputs. Rust/Go services remain authoritative for runtime and policy behavior.
GKE, networking, Cloud DNS, certificates, GitOps deployment state, and protected
release attestations remain owned by their separate repositories.

## Source evidence

The review exercised:

```bash
python3 ~/.agents/skills/protobuf/scripts/proto_guard.py protocols/proto
pnpm exec buf lint protocols
pnpm run generate:check
pnpm lint
pnpm typecheck
pnpm test
pnpm build
pnpm test:accessibility
tools/dev/nixw develop .#ci-bazel --command \
  tools/dev/bazelw test \
    //sdk/typescript:unit_tests //libs/ts/... //apps/console:unit_tests //apps/admin:unit_tests \
    --config=ci
tools/dev/nixw develop .#ci-bazel --command \
  tools/dev/bazelw build \
    //apps/console:release_archive //apps/admin:release_archive --config=ci
tools/dev/nixw develop .#default --command \
  python3 tools/analysis/run_architecture_checks.py
tools/dev/nixw develop .#ci-bazel --command \
  tools/dev/bazelw build //... --nobuild --config=ci
tools/dev/nixw develop .#default --command \
  mkdocs build -f docs/mkdocs.yml --strict
```

The static protobuf guard and Buf lint pass all 72 canonical schemas. SDK
generation is current and deterministic. Focused native TypeScript and Bazel
suites cover deadlines, cancellation, retries, pagination cycles, response and
event limits, session validation, CSP/headers, telemetry redaction, bounded PDB
parsing, responsive shell boundaries, administrative request policy, and
single-flight approval. Both optimized Next standalone archives build with
Bazel sandbox networking disabled.

The accessibility suite completed 70 scheduled cases across Chromium, Firefox,
WebKit, a Pixel 7 viewport, and a 600-by-800 reflow viewport: 54 applicable
checks passed and 16 project-specific checks were intentionally skipped. It
exercises representative routes with automated WCAG 2.1 AA rules, skip-link
focus, keyboard dismissal and focus return, 44-pixel mobile navigation targets,
and horizontal reflow. This is repeatable browser-source evidence; it does not
replace manual VoiceOver/NVDA testing or validation through the production IdP
and ingress.

The built archives were inspected for the root standalone entry point,
`public/robots.txt`, the middleware manifest, and static assets. Their local
source-artifact SHA-256 values were:

- Command: `5919fbeef15bf1045750b66ab8e00d3357dd421d94d6ccd526b3a63b172814ec`
- Governance: `5a7f506b60f91988d85b7c9e806f6b48561b67a5b2645c6fb513a6c18625219c`

Both packaged servers were started from the Bazel output tree. Representative
Command and Governance routes returned `200`, rendered nonce-bearing HTML, and
returned the expected CSP, cross-origin, permissions, referrer, no-sniff,
frame-denial, and no-store headers. Both packaged `robots.txt` files disallow
all crawlers. These hashes and observations identify local review artifacts;
they are not signed release provenance or evidence of a deployed environment.
The pinned aggregate architecture suite also passed all 21 repository
invariants. The final full Bazel repository graph analyzed 1,320 targets across
2,057 loaded packages and 89,702 configured targets with 16 aspect
applications, and the strict MkDocs site build completed without warnings.

## Compatibility

No protobuf wire schema was changed by this work. One stale Protobuf-ES output
was regenerated from the existing canonical `inference/v1/model.proto`, so the
TypeScript SDK now exposes its already-defined descriptor messages and enums.
The change is a generated TypeScript source-surface addition, not a field-number,
wire-type, package, service, or semantic schema change. The SDK remains private
until package provenance, signing, registry approval, and consumer canary pass.

## Connected qualification boundary

Source checks do not establish live identity, tenant isolation, service audit,
GKE health/drain behavior, load-balancer header enforcement, DNS/DNSSEC state,
certificate lifecycle, manual assistive-technology behavior, production-browser
behavior through the connected identity and ingress layers, SLOs, alerts,
canary safety, signed provenance, SBOM completeness, or rollback execution.
Those controls require protected identities and the owning infrastructure,
GitOps, IdP, API, accessibility, and release lanes. No cloud, DNS, cluster,
GitHub, package registry, or production mutation was performed during this
review.
