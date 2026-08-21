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

The built archives were inspected for the root standalone entry point,
`public/robots.txt`, the middleware manifest, and static assets. Their local
source-artifact SHA-256 values were:

- Command: `646876ef8be1aca569870aec856d1f3145341219380a76b448b5bf2d62cd0467`
- Governance: `5586eab1cb311747e26e73269e2e8c075d2eed8c9a5cc3a44a512e17b71c332a`

Both packaged servers were started from the Bazel output tree. Representative
Command and Governance routes returned `200`, rendered nonce-bearing HTML, and
returned the expected CSP, cross-origin, permissions, referrer, no-sniff,
frame-denial, and no-store headers. Both packaged `robots.txt` files disallow
all crawlers. These hashes and observations identify local review artifacts;
they are not signed release provenance or evidence of a deployed environment.
The pinned aggregate architecture suite also passed all 21 repository
invariants. The full Bazel repository graph analyzed 1,263 targets across
2,099 loaded packages and 87,425 configured targets, and the strict MkDocs
site build completed without warnings.

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
certificate lifecycle, accessibility across supported browsers, SLOs, alerts,
canary safety, signed provenance, SBOM completeness, or rollback execution.
Those controls require protected identities and the owning infrastructure,
GitOps, IdP, API, and release lanes. No cloud, DNS, cluster, GitHub, package
registry, or production mutation was performed during this review.
