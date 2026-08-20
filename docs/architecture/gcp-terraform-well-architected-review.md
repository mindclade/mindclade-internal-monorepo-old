# Google Cloud Terraform and Well-Architected review

**Review date:** 2026-08-20  
**Reviewer:** Codex, using the repository-requested Google Cloud and Terraform review rubrics  
**Repository / organization:** Mindclade monorepo / organization context not available locally  
**Scope:** `infra/terraform`, related architecture and security documentation, and Terraform CI/release controls  
**Environments:** development, staging, production, and the lifetime-scoped DNS hub  
**Workload tier:** Unknown; production intent is documented but not business-approved  
**Evidence window:** Local repository and public Google/HashiCorp documentation inspected 2026-08-20  
**Decision owner:** Cloud Platform with Security and Release Engineering approval

## 1. Executive decision

- **Recommendation:** CONDITIONAL GO for the reusable-module implementation; NO-GO for a production apply.
- **Current posture:** Implemented-but-unqualified module library. The repository contains substantive controls and offline tests, but it does not contain the live organization inputs, environment compositions, plans, state inventory, drift evidence, connected-provider qualification, or recovery evidence needed to claim a deployed landing zone.
- **Target posture:** Small blast-radius roots in the separately controlled live configuration, keyless and separated plan/apply identities, protected remote state, reviewed saved plans, additive IAM, private regional GKE, immutable data/evidence stores, tested SLO/restore controls, cost allocation, and retained release evidence.
- **Top three risks:** unknown live hierarchy/network/IAM/state; no production plan or provider-connected evidence; no approved RTO/RPO, restore/failover evidence, or complete merge-blocking qualification.
- **Top three actions:** decide and record the live topology; generate a reviewed non-production saved plan with policy/security/cost evidence; qualify restore, identity-deny, private-connectivity, SLO, and GPU/CPU workload behavior before promotion.
- **Confidence:** HIGH for repository observations, MEDIUM for target recommendations, LOW for cloud posture because no cloud or external live repository evidence was supplied.

The review uses the Google Cloud Well-Architected Framework's six pillars and AI/ML perspective, while preserving the repository's rule that source presence is not deployment evidence. See the [Google Cloud Well-Architected Framework](https://docs.cloud.google.com/architecture/framework) and [Terraform root-module guidance](https://docs.cloud.google.com/docs/terraform/best-practices/root-modules).

## 2. Scope, constraints, and assumptions

### In scope

- Terraform module contracts, resource safety controls, state boundaries, tests, and documentation.
- Resource hierarchy, IAM, networking, security, storage/data, Pub/Sub, GKE/AI execution, observability, reliability/DR, FinOps, CI, and software supply-chain implications.
- Static repository validation and mock-provider tests that do not access credentials or cloud state.

### Out of scope

- Terraform apply, import, state mutation, drift remediation, IAM changes, API enablement, bucket creation, or any other cloud mutation.
- The external `infrastructure-live` contents, active organization policy/ruleset state,
  billing data, quotas, and actual deployed resources. A locally available `github-config`
  declaration was prepared separately in evaluate mode but was not planned or applied.
- Speculative Vertex AI, BigQuery lakehouse, Dataflow, BigLake, Dataplex, VPC Service Controls, hybrid connectivity, or Shared VPC implementation without an approved requirement.

### Constraints

- The working tree contained unrelated user changes; this work does not overwrite or reformat those paths.
- Provider-connected validation may require downloading provider plugins. Offline mock tests prove configuration contracts, not API permissions, quotas, availability, performance, or live behavior.
- The environment roots cannot be safely composed until organization, billing, regions, CIDRs, principals, state, retention, SLO, and recovery decisions are supplied.

### Assumptions and unknowns

| ID | Statement | Classification | Validation needed | Owner |
|---|---|---|---|---|
| A-001 | GKE/Kueue/JobSet is the intended AI/ML execution plane; no managed Vertex AI control plane is required. | Observed | Architecture approval and workload qualification | AI Platform |
| A-002 | Deployment roots and sensitive values belong in a separately controlled live repository. | Observed | Inspect live repository and its rulesets | Cloud Platform |
| A-003 | DNS remains an in-repository lifetime-scoped exception applied before environment roots. | Observed | Confirm state owner and recovery procedure | Cloud Platform |
| A-004 | Regional GKE Standard remains the approved mode, and Terraform owns CPU pools while each environment chooses Terraform or `ComputeClass` for GPU pools. | Proposed | ADR plus connected GKE qualification | AI Platform |
| A-005 | Organization/folder/project taxonomy, state buckets, principals, CIDRs, regions, data residency, and compliance requirements are not discoverable here. | Unknown | Supply approved decision records and live inputs | Cloud Platform / Security |
| A-006 | 99.9% component objectives are provisional and do not establish workload RTO/RPO. | Observed | Business-impact analysis and restore tests | Service Owners |
| A-007 | Reusable presubmit and CodeQL workflows are SHA-pinned; external ruleset activation and release canary evidence remain unavailable locally. | Observed / Unknown | Inspect the reviewed workflow source, observe real check runs, and qualify the external ruleset/release path | Release Engineering |
| A-008 | Current GKE version, accelerator availability, provider behavior, quotas, and support entitlements will remain available in selected regions. | Unknown | Connected qualification immediately before rollout | AI Platform |

## 3. Current-state evidence

| Evidence ID | Source | Scope | Collected at | Redaction | Notes |
|---|---|---|---|---|---|
| E-001 | `infra/terraform/modules/**` | 32 module directories | 2026-08-20 | None | 22 substantive modules and 10 blueprint-named scaffolds at review start |
| E-002 | `infra/terraform/environments/**` | Four roots | 2026-08-20 | None | Only `dns_hub` is materialized; development/staging/production remain non-deployable |
| E-003 | `docs/blueprint/production-monorepo-blueprint.md` and `docs/architecture/**` | Workload and repository laws | 2026-08-20 | None | Bazel owns build/release; Nix owns toolchains; GKE is the documented AI execution substrate |
| E-004 | `.github/workflows/presubmit.yml`, `security.yml`, `release.yml` | CI and release | 2026-08-20 | None | Terraform contracts, two-version compatibility, TFLint, Trivy, plan-policy fixtures, and interface drift run in CI; connected-plan, cost, nightly, and GPU evidence are incomplete |
| E-005 | `components.toml`, `architecture/component_ownership.toml`, `maturity.toml` | Maturity governance | 2026-08-20 | None | Terraform modules and `dns_hub` are registered with tier-2 ownership and executable governance tests; deployment promotion evidence remains absent |
| E-006 | `terraform fmt -check -recursive infra/terraform` | Static formatting baseline | 2026-08-20 | None | Passed before implementation |
| E-007 | `actionlint` and `yamllint --strict .github/workflows` | Workflow syntax baseline | 2026-08-20 | None | Passed before implementation |
| E-008 | [Google Cloud WIF deployment guidance](https://docs.cloud.google.com/iam/docs/workload-identity-federation-with-deployment-pipelines) | CI identity design | 2026-08-20 | N/A | Subject mapping and attribute conditions are required trust controls |
| E-009 | [Cloud Storage public access prevention](https://docs.cloud.google.com/storage/docs/public-access-prevention), [versioning](https://docs.cloud.google.com/storage/docs/object-versioning), and [lifecycle controls](https://docs.cloud.google.com/storage/docs/control-data-lifecycles) | Storage controls | 2026-08-20 | N/A | Supports the private, recoverable, lifecycle-governed baseline |
| E-010 | [Cloud Monitoring metrics scopes](https://docs.cloud.google.com/monitoring/settings) | Multi-project observability | 2026-08-20 | N/A | Scoping projects can observe monitored projects without duplicating service SLO logic |
| E-011 | [Cloud Audit Logs best practices](https://docs.cloud.google.com/logging/docs/audit/best-practices) | Audit/evidence | 2026-08-20 | N/A | Centralized routing and protected retention are required beyond default logs |
| E-012 | [Pub/Sub exactly-once delivery](https://docs.cloud.google.com/pubsub/docs/exactly-once-delivery) and [retry policy](https://docs.cloud.google.com/pubsub/docs/subscription-retry-policy) | Event delivery | 2026-08-20 | N/A | Delivery semantics do not replace consumer idempotence and replay testing |
| E-013 | Backendless Terraform initialization, provider-schema validation, and mock tests | 36 configurations / 33 suites | 2026-08-20 | None | 36/36 configurations validate; 237/237 mock runs pass; all 33 suites pass at Google 7.41.0 and 7.45.0 |
| E-014 | TFLint 0.64.0, Checkov 3.3.0, Trivy 0.74.0, and Conftest 0.63.0 | Terraform static/security policy | 2026-08-20 | None | TFLint passed 36/36; Checkov reported 152 passed, 0 failed, 8 documented skips; Trivy reported zero unsuppressed findings with three expiring exceptions; 22/22 OPA tests and integration fixtures pass |
| E-015 | Actionlint, strict workflow YAML lint, diff check, Nix flake evaluation, and repository static presubmit | CI/repository integration | 2026-08-20 | None | Terraform/workflow checks pass and gates were strengthened; a final shared-worktree static rerun is blocked by unrelated concurrent Rust/Bazel dependency alignment changes |

## 4. Architecture and control assessment

| Finding | Area | Status | Severity | Evidence | Blast radius | Confidence |
|---|---|---|---|---|---|---|
| F-001 | Terraform module structure | PASS | High | E-001, E-006, E-013, E-014 | Every module consumer | High |
| F-002 | Root/state architecture | FAIL | Critical | E-002 | Organization or whole environment per state mistake | High |
| F-003 | Resource hierarchy and tags | PARTIAL | High | E-001, E-005 | Organization-wide policy and allocation | High |
| F-004 | Keyless least-privilege identity | PARTIAL | Critical | E-001, E-008 | CI/deployment and workload impersonation | High |
| F-005 | Network isolation and IPAM | PARTIAL | High | E-001, A-005 | Shared connectivity and data exfiltration paths | High |
| F-006 | Security governance/audit | PARTIAL | High | E-001, E-004, E-011 | Organization-wide evidence and detection | High |
| F-007 | Storage and data lifecycle | PARTIAL | High | E-001, E-009 | Training data, artifacts, checkpoints, and evidence | High |
| F-008 | Pub/Sub data/event plane | PARTIAL | High | E-001, E-012, E-013 | Durable workflow events and replay | High |
| F-009 | GKE AI/ML platform | PARTIAL | High | E-001, E-003 | Regional training/inference capacity | High |
| F-010 | Observability/SLOs | PARTIAL | High | E-001, E-010 | Cross-project incident detection | High |
| F-011 | Reliability and disaster recovery | FAIL | Critical | A-006 | Data loss and prolonged outage | High |
| F-012 | FinOps | PARTIAL | Medium | E-001, A-005 | Organization spend and GPU efficiency | High |
| F-013 | CI and software supply chain | PARTIAL | High | E-004, A-007 | All merged/released changes | High |
| F-014 | Compliance/residency | UNKNOWN | High | A-005 | Regulated or restricted data estate | High |
| F-015 | Production-claim discipline | PASS | Medium | E-003, E-005 | Repository-wide reporting accuracy | High |

### Key observations

- Existing modules already implement strong primitives: private regional GKE, Dataplane V2, Workload Identity Federation for GKE, Binary Authorization enforcement, protected GCS, KMS, Secret Manager metadata-only ownership, private SQL/Redis, aggregated logging, SCC exports, request SLOs, and burn-rate alerts.
- The ten initially empty directories are now materialized without creating duplicate authorities. `object_storage`, `observability`, and `audit_archive` compose `storage`, `monitoring`, and `log_sink`; the other modules own distinct hierarchy, identity, event, compute, and cache contracts.
- Terraform cannot prove client-side manifest-last publication, Pub/Sub consumer idempotence, Kubernetes RBAC/network policy, restore correctness, accelerator availability, or service-agent permissions from mock plans. These require connected and runtime evidence.
- Network modules intentionally exclude authoritative IPAM, Shared VPC host setup, firewall policy, PSC, hybrid networking, IPv6, and DNS policy. These are unresolved architecture choices, not safe defaults to guess.
- Budgets, labels, GKE cost allocation, autoscaling, spot opt-in, and cache lifecycle are useful controls, but no billing export, cost-center taxonomy, anomaly routing, unit economics, commitment strategy, or telemetry budget is approved.

## 5. Target-state design

The minimum safe dependency graph separates state by lifetime, authority, and blast radius:

```text
manual/seed bootstrap
  -> organization + folders + tags + org policy
     -> audit archive + security services
     -> CI/workload identity
        -> DNS hub (independent lifetime)
        -> network/IPAM roots
           -> environment projects and regional GKE
              -> data services and object/event planes
              -> CPU/GPU/build execution pools
                 -> workload/GitOps configuration
     -> metrics scopes, SLOs, budgets, drift and recovery evidence
```

No root should combine resources merely because they share an environment. Follow Google guidance to keep root modules small—ideally a few dozen resources—and split state when lifecycle, permissions, failure domains, or approval owners differ.

| State slice | Authority | Inputs from prior state | Must not own |
|---|---|---|---|
| Seed/bootstrap | State projects, buckets, Terraform identities | Human break-glass approval | Workloads, broad user keys |
| Organization | Tags, folders, additive org IAM, org policies | Organization/billing decisions | Projects' application resources |
| Audit/security | Aggregated sinks, archive, SCC/BinAuthz/KMS metadata | Organization IDs and protected destination projects | Secret payloads or application deploys |
| Identity | OIDC pools/providers, dedicated GSAs, additive impersonation | Approved issuer/subject/branch/environment claims | Service-account keys or authoritative IAM |
| Network | VPC/subnets/NAT/DNS/PSA after approved IPAM | Regions, CIDRs, flow matrix | GKE workloads or database schemas |
| Environment platform | Projects and private regional GKE | Hierarchy, identity, networking | DNS delegation and org-wide policy |
| Data/event | Protected buckets, Pub/Sub, SQL/Redis | Classification, residency, retention, RPO | Dataset publication semantics in Terraform |
| Observability | Metrics scopes, SLOs, alerts, dashboards | Service owners, SLI filters, channels | Application instrumentation |
| Build execution | CPU pools and cache access identity | GKE, cache buckets, pinned worker image | Kubernetes executor deployment or Bazel policy |

Identity paths are keyless: GitHub OIDC claims are restricted at the workload identity provider, dedicated service accounts receive only additive role members, apply identities are distinct from plan identities, and GKE KSAs use WIF rather than exported keys. The live repository must verify effective inherited IAM and deny both an untrusted branch and an unbound KSA.

Data paths keep source/artifact identity separate from location. Raw, canonical, curated, model-ready, checkpoints, release evidence, Bazel CAS, and Nix closures use distinct policy classes. Protected data/evidence retains recovery controls; rebuildable caches use explicit bounded lifecycle and cannot silently become an archive.

### Decision records

| Decision | Selected option | Alternatives | Tradeoff | Revisit trigger |
|---|---|---|---|---|
| D-001 | Reusable modules here; deployable environment values/state in controlled live roots | Commit production tfvars here | Keeps secrets/topology and promotion authority separate | Live-repository ownership changes |
| D-002 | Additive `*_iam_member` resources only | Authoritative policies/bindings | Avoids clobbering unrelated principals; more resources | Central IAM policy compiler adopted |
| D-003 | Workload Identity Federation; no service-account keys | JSON keys | Short-lived and claim-bound; requires careful claims/IAM tests | Provider cannot support federation |
| D-004 | GKE Standard/Kueue execution, not speculative Vertex AI | Vertex AI managed jobs | Matches repository workload contracts; more platform operations | Approved managed-service requirement |
| D-005 | Composition modules preserve one resource authority | Duplicate bucket/monitoring/sink implementations | Avoids state conflicts; adds a thin abstraction layer | Canonical module APIs are retired |
| D-006 | Bazel CAS, Nix cache, platform artifacts, and audit evidence remain distinct | One shared bucket/cache | Clear retention, IAM, and deletion semantics | Formal policy proves equivalence |
| D-007 | No development/staging/production root materialization without approved topology | Guess generic defaults | Prevents false production posture; delays deployability | Required decisions A-005 are supplied |
| D-008 | No apply, import, or state mutation in this change | Opportunistic cloud rollout | Keeps review reversible and evidence-based | Explicit protected-environment approval |

## 6. Remediation roadmap

| Priority | Action | Finding | Owner type | Prerequisite | Validation | Rollback | Cost effect |
|---|---|---|---|---|---|---|---|
| P0 | Approve hierarchy, state, identity, region, CIDR, retention, compliance, SLO, RTO/RPO, and cost-allocation records | F-002, F-003, F-014 | Architecture/Security/FinOps | Business requirements | Signed ADRs and threat/data-flow diagrams | Revert proposed records before apply | Planning only |
| P0 | Materialize isolated non-production live roots with protected GCS state and separated keyless plan/apply identities | F-002, F-004 | Cloud Platform | Approved records | Backend recovery drill; WIF allow/deny tests | Restore versioned state; revoke grants | State/logging cost |
| P0 | Produce and retain a saved non-production plan plus JSON, destructive/replacement/IAM/public-access summary, policy/security scans, and cost estimate | F-001, F-013 | Release Engineering | Live roots | Two-person plan review; zero unapproved destructive/public changes | Discard plan; no apply | CI minutes |
| P0 | Define and exercise backup/restore/failover per data class and workload tier | F-011 | Service/Data owners | Approved RTO/RPO | Measured restore and failback evidence | Abort game day; restore primary | Backup/standby cost |
| P1 | Complete private connectivity/IPAM/firewall/Shared VPC decisions and test allowed/denied flows | F-005 | Network/Security | Topology decisions | Connectivity tests and VPC flow-log evidence | Revert isolated root plan | NAT/PSC/logging cost |
| P1 | Deploy metrics scope, service SLOs, burn alerts, audit access detection, and budget/anomaly routing | F-010, F-012 | SRE/FinOps | Owners/channels/SLIs | Alert fire/recovery and dashboard checks | Disable individual policies | Telemetry cost |
| P1 | Qualify Pub/Sub replay, DLQ/redrive, duplicate handling, ordering, and backpressure | F-008 | Data Platform | Consumer implementation | Failure injection and bounded backlog tests | Stop publishers; replay retained data | Retention/egress cost |
| P1 | Qualify CPU/GPU separation, autoscaling, spot interruption, cache corruption/miss, and immutable worker image | F-009, F-013 | AI Platform/Release | Cluster/quota/images | Connected nightly/GPU evidence | Scale pool to zero; use local execution | Compute/cache cost |
| P2 | Add billing export, allocation taxonomy, unit metrics, commitment review, and telemetry budgets | F-012 | FinOps | Billing/project taxonomy | Reconciled showback and anomaly test | Disable export consumers | Export/query cost |
| P2 | Activate the SHA-pinned workflow checks after the staged ruleset observes exact `lint`/`terraform` contexts | F-013 | Release Engineering | External workflow/ruleset access | Ruleset positive/negative test and release canary | Return the new ruleset to evaluate before workflow rollback | CI minutes |

## 7. Validation plan

### Repository gate

1. Run `terraform fmt -check -recursive infra/terraform` and `git diff --check`.
2. For every module, run backendless `terraform init -lockfile=readonly`, `terraform validate`, and mock-provider `terraform test`; lock updates are reviewed separately.
3. Run `actionlint`, strict YAML lint, repository static checks, and the relevant Bazel filegroup/query checks.
4. Keep the enforced invariant that every materialized module has constraints, README, outputs, a committed provider lock, and mock tests; do not silently skip directories without tests.
5. Keep Nix-pinned TFLint, Trivy, Conftest fixtures, provider compatibility, and interface drift blocking. Run the saved-plan policy against an approved live profile, then retain replacement/destruction, cost, and policy reports in controlled CI.

### Connected non-production gate

1. Authenticate through WIF with a plan-only service account. Prove allowed subject/environment claims succeed and an untrusted repository, branch, and pull request fail.
2. Save a plan, export JSON, scan it, summarize IAM/public access/deletions/replacements, estimate cost, and have a second reviewer approve the exact artifact digest.
3. Apply only through a protected environment using a distinct apply identity. Run a second plan and require empty drift.
4. Verify private GKE control-plane reachability only from approved management paths; verify forbidden paths, metadata concealment, Binary Authorization denial, KSA isolation, and no public bucket/topic IAM.
5. Test Pub/Sub duplicate/retry/DLQ/redrive, Storage generation preconditions and recovery, database PITR, GKE backup restore if adopted, and archive read access through break-glass procedure.
6. Fire and recover SLO burn alerts, audit-access alerts, quota alerts, and budget/anomaly notifications. Record delivery latency and owner acknowledgement.
7. Run CPU/high-memory and GPU qualification with pinned worker images, cache cold/warm/corrupt scenarios, capacity exhaustion, spot/preemption, cancellation, and scale-to-zero behavior.

Acceptance requires no unexplained drift, no unapproved public access or basic/admin grants, no service-account keys, no destructive/replacement action without explicit approval, recovery within approved RTO/RPO, and retained evidence linked to the reviewed commit and plan digest.

## 8. Approval-gated changes

| Change | Scope | Risk | Approver | Execution window | Rollback |
|---|---|---|---|---|---|
| C-001 | Create/move organization folders, projects, tags, or org policy | Organization-wide | Cloud Platform + Security | Change window | Revert policy where safe; hierarchy moves need explicit backout |
| C-002 | Bootstrap or migrate Terraform state/backends | Entire state slice | Cloud Platform | No concurrent applies | Restore versioned object and previous backend config |
| C-003 | Create WIF pools/providers or grant impersonation/project roles | CI/workload trust boundary | Security | Protected identity window | Disable provider/revoke additive members |
| C-004 | Apply network/firewall/NAT/DNS/PSA changes | Connectivity | Network + Security | Maintenance window | Apply reviewed inverse plan or route rollback |
| C-005 | Create or change GKE clusters/node pools/backups | Regional compute/workloads | AI Platform + SRE | Capacity window | Roll back image/config; preserve protected cluster/state |
| C-006 | Lock bucket retention policy or change lifecycle/deletion protection | Potentially irreversible data retention | Data owner + Legal/Security | Data change window | Retention lock has no rollback; require exact acknowledgement |
| C-007 | Apply database/Redis replacement, failover, restore, or CMEK change | Stateful services | Data owner + SRE | Maintenance window | Restore/failback runbook |
| C-008 | Change required GitHub rulesets or release identities | Every merge/release | Release Engineering + Security | Repository change window | Restore prior immutable workflow/ruleset revision |

## 9. Residual risk and follow-up

- This repository cannot establish live posture. Effective IAM inheritance, org policies, state controls, drift, service agents, quotas, regional capacity, billing, SCC tier, audit coverage, and rulesets remain unknown until separately evidenced.
- `prevent_destroy` and provider deletion policies reduce accidental Terraform destruction but do not replace GCP retention locks, backup copies, break-glass restrictions, or API-level deletion protection.
- Exactly-once Pub/Sub affects delivery within its supported scope; consumers still require idempotency, fencing, replay, and failure evidence.
- Mock providers can accept impossible combinations. Current provider schemas and APIs must be exercised in an isolated project before production.
- The pinned GKE/platform tuple and accelerator profiles are time-sensitive. Requalification—not a casual variable change—is the upgrade path.
- A production decision remains blocked until the live roots, approval owners, RTO/RPO, residency/compliance controls, and validation evidence are supplied.

## 10. Evidence appendix

Sanitized repository commands used during review:

```text
rg --files infra/terraform
terraform version
terraform fmt -check -recursive infra/terraform
terraform -chdir=infra/terraform/modules/<module> init -backend=false -lockfile=readonly
terraform -chdir=infra/terraform/modules/<module> validate -no-color
terraform -chdir=infra/terraform/modules/<module> test -no-color
tflint --chdir=infra/terraform/modules/<module>
checkov -d infra/terraform/modules --framework terraform --skip-download
nix develop .#ci --command trivy config --disable-telemetry --exit-code 1 --severity UNKNOWN,LOW,MEDIUM,HIGH,CRITICAL --skip-check-update --skip-dirs '**/.terraform' infra/terraform
ci/terraform/check.sh contracts
ci/terraform/check.sh compat
infra/terraform/policy/test-policy.sh
infra/terraform/governance/check.sh
NO_COLOR=1 actionlint
yamllint --strict .github/workflows
PYTHONDONTWRITEBYTECODE=1 python3 ci/presubmit/pipeline.py --static-only
```

Observed local Terraform version: `1.15.8` on `darwin_arm64`. All 32 modules passed backendless provider-schema validation and 227 mock test runs; TFLint passed; Checkov reported 152 passed, 0 failed, and 8 documented skips; and Nix-pinned Trivy reported zero unsuppressed findings at every severity with three source-local exceptions expiring 2027-08-20. No `terraform apply`, import, refresh, state command, credential command, API enablement, IAM mutation, or other cloud-changing command was run. Live qualification remains governed by `infra/terraform/PRODUCTION_READINESS.md`.
