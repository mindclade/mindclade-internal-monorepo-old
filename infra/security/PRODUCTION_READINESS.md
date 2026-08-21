# Infrastructure security production readiness

**Decision:** source control catalog implemented; production activation blocked.  
**Last repository review:** 2026-08-21.

## Repository evidence

| Gate | Status | Evidence or blocker |
|---|---|---|
| Versioned stable contract | PASS | JSON Schema `v1`, exact nine-document inventory, no implicit defaults or coercion |
| Ownership and review | PASS | `security-platform`; Platform and Security `CODEOWNERS` routing |
| Terraform enforcement links | PASS | Referenced module sources and mock suites exist; the complete minimum-provider matrix passed in this review |
| Kubernetes enforcement links | PASS | Native CEL admission, restricted namespaces, default-deny network policy, positive and negative fixtures exist and pass hermetic validation |
| CI supply-chain links | PASS | Security pipeline, artifact registry, Binary Authorization, immutable-image admission, and their source tests are linked |
| Fail-closed policy projection | PASS | Python schema/link validation plus OPA unit and catalog policy targets |
| Live effective IAM and break glass | BLOCKED | Requires exact principals, inherited-policy review, time-bounded approval exercise, audit query, and revocation proof |
| Live image and node attestation | BLOCKED | Requires trusted-root verification, Binary Authorization allow/deny, node image/firmware inventory, and connected GKE evidence |
| Live network isolation | BLOCKED | Requires approved CIDRs/identities plus allowed and denied flow evidence across VPC, DNS, perimeter, cluster, and telemetry paths |
| Live audit retention and recovery | BLOCKED | Requires sink health, protected query, retention verification, restore rehearsal, and alert fire/resolve evidence |
| Live secret rotation and weight access | BLOCKED | Requires key rotation/overlap/revocation, workload identity proof, model-weight allow/deny, and audit evidence |
| Drift and rollback rehearsal | BLOCKED | Requires exact deployed revision, empty post-change drift, forward-fix/revert exercise, named owners, and retained receipts |

## Activation gate

No control may be treated as active from this package alone. An environment owner must bind the same
immutable source revision and qualification digest to a reviewed deployment, then retain every
`requiredEvidence` item named by the corresponding catalog document. Missing, expired, mismatched,
or unverifiable evidence denies activation.

Production additionally requires the Terraform, Kubernetes, GitOps, observability, disaster
recovery, and release gates to agree for the same revision. An approved exception must be scoped,
time-bounded, independently reviewed, observable, and accompanied by revocation instructions; an
exception never changes the source contract's default.

## Rollback and incident boundary

- Stop before activation when identity, revision, evidence digest, policy result, or owner approval
  disagrees. Never edit retained evidence to make it match.
- Prefer a reviewed forward fix. Where exact-revision rollback is safe, preserve CRDs, audit logs,
  attestations, and the failed revision as evidence.
- Contain identity, signing, key, and model-weight incidents by revoking or disabling the narrow
  principal/trust path. Do not create long-lived keys or bypass admission as a recovery shortcut.
- This agent session does not apply Terraform, change GitHub governance, promote GitOps, access a
  production cluster, sign artifacts, or exercise emergency access.
