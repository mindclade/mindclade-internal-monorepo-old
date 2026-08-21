# Infrastructure security control catalog

**Status:** implemented source contract; environment activation and production qualification are
blocked. **Owner:** `security-platform` with Platform review and Security review through
`CODEOWNERS`.

This package is the source-owned join between the reusable Terraform controls, Kubernetes native
admission and network policy, and CI supply-chain gates already implemented elsewhere in the
monorepo. It does not duplicate those resources. Each JSON-compatible YAML document identifies the
authoritative enforcement sources, tests, connected evidence, failure behavior, activation gate,
rollback rule, and explicit non-responsibilities for one control family.

## Contract and authority

`control-contract.schema.json` is the canonical `v1` interface. The nine catalog records cover audit
retention, break-glass access, image policy, model-weight access, network policy, node attestation,
pod security, secret rotation, and software supply chain. `security_contracts.py` validates the
schema plus exact file inventory, sorted/unique fields, safe repository-relative paths, existence of
every enforcement and test source, independent ownership, retry bounds, and aggregate plane
coverage.

The catalog accepts only metadata and repository paths. Its output is a deterministic pass/fail and
diagnostic list; it does not default, coerce, mutate, deploy, query cloud state, or read credentials.
Schema compatibility follows the exact `schemaVersion`; incompatible changes require a new version
and consumer migration rather than weakening `v1`.

The OPA policy is an independently executed fail-closed projection of the lifecycle invariants.
`kyverno/kustomization.yaml` is deliberately an empty compatibility sentinel: the authoritative GKE
admission engine is Kubernetes native CEL `ValidatingAdmissionPolicy` under
`infra/kubernetes/platform/security`, so this package cannot silently introduce Kyverno as a second
policy engine.

## Failure, retry, and rollback

- Missing or stale evidence denies activation. Validation errors never become warnings.
- Denied identity, image, pod, node, or supply-chain decisions are not retried automatically.
- Transient audit, network, and rotation checks permit at most three bounded attempts; emergency
  access remains a manual decision.
- Rollback uses a reviewed exact revision or a forward fix. Identity and key incidents use manual
  containment. Every path preserves audit evidence and requires a named environment owner.

## Validation

Run through the pinned Nix/Bazel toolchain:

```console
tools/dev/nixw develop .#ci-bazel --command tools/dev/bazelw test //infra/security:all --config=ci
```

The Python targets provide schema/link and behavior coverage. The shell target captures the
Nix-owned Conftest binary as a declared Bazel runfile, runs OPA unit tests, and evaluates all nine
catalog documents.

## Production boundary

This repository owns reusable source and qualification gates, not live GCP resources, GitHub
rulesets, production Argo CD revision selection, credentials, or cluster access. A catalog pass means
the source contract is coherent. It does not prove effective IAM, network reachability, audit-log
delivery, key rotation, admission enforcement, signature validity, alert delivery, restore behavior,
or revocation in any environment. Those blockers and required evidence are recorded in
`PRODUCTION_READINESS.md`.
