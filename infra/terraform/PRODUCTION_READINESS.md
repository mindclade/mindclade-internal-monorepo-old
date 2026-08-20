# Terraform production readiness

**Current decision:** Not ready for production apply.  
**Implementation status:** All 32 reusable modules are materialized and pass local
formatting, provider-schema validation, mock tests, TFLint, and Checkov; environment
topology and live evidence remain external and unknown.
**Last repository review:** 2026-08-20.

## Promotion evidence

| Gate | Status | Required evidence |
|---|---|---|
| Stable module contracts and owners | PARTIAL | Module READMEs/tests plus infrastructure entries in `components.toml` and approved reviewers |
| Formatting and static repository checks | PASS | Terraform formatting, diff check, Actionlint, strict YAML lint, and repository static presubmit passed locally on 2026-08-20 |
| Backendless init/validate/mock tests | PASS | 32/32 modules initialized with no backend, schema-valid, and 226/226 mock runs passed using committed locks |
| Provider-connected non-production plan | MISSING | Saved plan, plan JSON, provider versions, project/region, and expiration |
| IaC security and policy checks | PARTIAL | Local TFLint passed and Checkov 3.3.0 reported 152 passed, 0 failed, 8 documented skips; CI pinning, policy-as-code, and retained reports remain missing |
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

The 2026-08-20 repository gate used Terraform 1.15.8 on `darwin_arm64` and
checksum-verified Google providers pinned by each module lock file (7.44.0 or 7.45.0).
Initialization used `-backend=false`; no credentials, remote state, refresh, plan against
GCP, apply, import, state mutation, or cloud API operation was used.

- `terraform fmt -check -recursive infra/terraform`: pass.
- Backendless `terraform init -lockfile=readonly` and `terraform validate`: 32/32 pass.
- `terraform test` with `mock_provider`: 226/226 runs pass across 32/32 modules.
- TFLint 0.63.1 default Terraform rules: pass across 32/32 modules.
- Checkov 3.3.0 Terraform scan: 152 passed, 0 failed, 8 documented skips.
- Actionlint, strict workflow YAML lint, `git diff --check`, and
  `python3 ci/presubmit/pipeline.py --static-only`: pass.

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
