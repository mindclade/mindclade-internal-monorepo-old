# Terraform production readiness

**Current decision:** Not ready for production apply.  
**Implementation status:** All 44 reusable modules are materialized. The prior whole-tree
qualification baseline covered 43 modules plus the now-retired `dns_hub` deployable root;
the new Certificate Manager module and retired-root fixture pass scoped contracts,
provider-schema validation, mock tests, and generated-interface governance. A fresh
whole-tree qualification run is required before release. Environment topology and live
evidence remain external and unknown.
**Last repository review:** 2026-08-21.

## Promotion evidence

| Gate | Status | Required evidence |
|---|---|---|
| Stable module contracts and owners | PASS | Generated interfaces for 44 modules, versioned migration evidence, component ownership, and Platform/Security review routing; `dns_hub` is retained only as a non-deployable DNS module test fixture |
| Formatting and static repository checks | PASS | Terraform formatting, diff check, Actionlint, strict YAML lint, Nix flake evaluation/toolchain evidence, documentation build, and the repository static presubmit pass |
| Backendless init/validate/mock tests | PARTIAL | The 2026-08-20 baseline passed 47/47 configurations and 261/261 mock runs across 44 suites; current scoped Certificate Manager and DNS module tests pass, but the post-retirement whole-tree gate has not been rerun |
| Provider-connected non-production plan | MISSING | Saved plan, plan JSON, provider versions, project/region, and expiration |
| IaC security and policy checks | PARTIAL | CI pins TFLint 0.64.0 and Trivy 0.74.0; Trivy has zero unsuppressed findings, three resource-local exceptions, and one module-scoped embedded-check exception, all expiring 2027-08-20. Conftest's fail-closed policy and fixtures pass, and local Checkov reported 152 passed, 0 failed, 8 documented skips. An approved live profile, saved-plan evaluation, cost analysis, and retained reports remain missing |
| IAM and public-access review | MISSING | Effective inherited IAM; WIF allow/deny; no keys/basic/public grants |
| Destructive/replacement review | MISSING | Explicit zero/unapproved-action statement or signed approvals |
| Cost review | MISSING | Estimate, budgets, allocation tags/labels, quota and capacity assumptions |
| Private connectivity and security tests | MISSING | Allowed/denied flow evidence, audit routing, Binary Authorization and KSA isolation |
| Data/event recovery | MISSING | Storage restore, Pub/Sub replay/DLQ/idempotence, SQL PITR, Redis recovery evidence |
| GKE CPU/GPU qualification | MISSING | Pinned image/platform, autoscaling, preemption/cancellation, capacity and cache evidence |
| SLO/alert qualification | MISSING | Alert fire/recovery, dashboard, notification owner, telemetry cost |
| RTO/RPO and DR exercise | MISSING | Approved objectives plus timed restore/failover/failback |
| Post-apply drift | MISSING | Empty reviewed plan after an approved non-production apply |

`terraform test` with a mocked provider is a contract test. It cannot prove that APIs
are enabled, service agents hold required grants, quotas/capacity exist, organization
policy permits a resource, a restore succeeds, or a workload meets its SLO.

## Local validation evidence

The counts below are the 2026-08-20 pre-retirement repository baseline. They are retained
as dated evidence and are not a claim about the current 44-module tree. The current DNS
and certificate change has separate scoped validation; a new whole-tree run must replace
this baseline before release.

The 2026-08-20 repository gate used Terraform 1.15.8 on `darwin_arm64` and
checksum-verified Google providers. All 44 deployable-unit locks select reviewed version
7.45.0; canonical three-platform fixtures qualify both the declared minimum 7.41.0 and
reviewed 7.45.0. Initialization used `-backend=false`; no credentials, remote state,
refresh, plan against GCP, apply, import, state mutation, or cloud API operation was used.

- `terraform fmt -check -recursive infra/terraform`: pass.
- The checked-in driver contract, deterministic discovery, cache trust boundary, and lock
  mutation regression tests pass.
- Backendless `terraform init -lockfile=readonly` and `terraform validate`: 47/47 pass.
- `terraform test` with `mock_provider`: 261/261 runs pass across 44/44 suites.
- Google provider compatibility: validate and 44/44 suites pass at both 7.41.0 and 7.45.0.
- TFLint 0.64.0 default Terraform rules: pass across 47/47 configurations.
- Warm local mock-suite comparison: one worker took 10.50 s and four workers took 4.18 s
  (same initialized tree/provider cache), supporting the bounded default of four without
  claiming a hosted-runner performance guarantee.
- Checkov 3.3.0 Terraform scan: 152 passed, 0 failed, 8 documented skips.
- Nix-pinned Trivy 0.74.0 scanned every severity with zero unsuppressed
  misconfigurations; the three source-local exceptions document generic encryption or
  audit-policy ownership and expire 2027-08-20.
- Conftest 0.63.0 / OPA 1.9.0: 22/22 unit tests plus saved-plan integration fixtures pass.
- Terraform interface governance: 44 units, 366 changes from v0.1.1, including 131 breaking
  changes covered by the planned v0.4.0 migration record; both hermetic Bazel tests pass.
- Actionlint, strict workflow YAML lint, `git diff --check`, and
  `nix flake check --no-build`: pass.
- `python3 ci/presubmit/pipeline.py --static-only`: all 21 repository checks pass.
- Strict MkDocs build and `nix fmt -- --ci`: pass.

The `lint` and `terraform` workflow jobs now have stable names. Their sibling
`github-config` ruleset is deliberately scoped to `mindclade` and defaults to `evaluate`;
activation remains blocked on observing those exact check-run contexts and proving an
intentional Terraform failure prevents merge. No ruleset was applied during this work.

## Required decisions before live-root materialization

- Organization, billing account, folders, project taxonomy, ownership, and lifecycle.
- Remote-state project/bucket/prefix/CMEK/retention/access/recovery and separated
  keyless plan/apply identities.
- Human, CI, deploy, break-glass, GKE node, and workload principal matrix, including
  exact GitHub issuer/subject/branch/environment attribute conditions.
- Regions/zones, residency, CIDRs, secondary ranges, control-plane ranges, Shared VPC,
  firewall/flow matrix, NAT/PSC/PSA/hybrid connectivity, and DNS authority.
- Data classes, lifecycle/legal hold/deletion, replication, Pub/Sub replay windows,
  and client-side immutable publication protocol.
- Workload tiers, SLOs, alert owners, RTO/RPO, backup/restore cadence, failover
  authority, standby/capacity strategy, and game-day schedule.
- Billing export, cost centers, allocation labels/tags, budgets/anomalies, GPU unit
  metrics, commitments, and telemetry budgets.
- Compliance obligations, VPC Service Controls or deny-policy requirements, SCC
  tier/detectors, audit retention, and privileged/break-glass process.

## Apply gate

An apply is authorized only when all of the following refer to the same immutable
commit and saved-plan digest:

1. a protected non-production root and remote backend are identified;
2. the plan identity is keyless and cannot apply; the apply identity is keyless and
   usable only through a protected environment;
3. validation, policy, security, IAM/public-access, cost, and destructive/replacement
   reports are retained;
4. Cloud Platform and required Security/Data/SRE owners approve the exact plan;
5. rollback and recovery steps are executable and an operator is named;
6. post-apply functional, security, SLO, and empty-drift checks are scheduled.

Production requires the same gates plus completed non-production qualification,
approved RTO/RPO, restore evidence, capacity/quota evidence, and a change window.

## Rollback and incident boundary

- Stop before apply when plan identity, backend, plan digest, approvals, or evidence do
  not match. Regenerate stale plans; never edit a saved plan.
- Prefer a reviewed forward fix. State rollback alone does not roll back cloud
  resources and can make Terraform forget real infrastructure.
- Recover state only from a verified object version under a no-concurrent-apply
  incident procedure; preserve the failed state and plan as evidence.
- Do not bypass `prevent_destroy`, provider deletion policies, retention locks, or
  organization policy during incident response without the resource/data owner and a
  documented break-glass record.
- Revoke additive WIF/IAM grants or disable the provider to contain identity issues;
  do not create a service-account key as a fallback.

The detailed findings, target state, validation plan, approval gates, and residual
risks are recorded in
[`docs/architecture/gcp-terraform-well-architected-review.md`](../../docs/architecture/gcp-terraform-well-architected-review.md).
