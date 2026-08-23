# Terraform on Google Cloud

- **Status:** Reusable module implementation; not a deployed landing zone.
- **Owners:** Cloud Platform; Security reviews identity, policy, audit, and supply-chain changes; Release Engineering owns CI promotion controls.
- **Provider contract:** Terraform `>= 1.9, < 2.0`; Google provider `>= 7.41, < 8.0`.

This directory implements the Google Cloud primitives and opinionated compositions
used by Mindclade's GKE-based AI/ML platform. It does not contain production values,
credentials, a production plan, live state inventory, or cloud qualification
evidence. File presence and mock-provider tests are not production claims; see
[`PRODUCTION_READINESS.md`](PRODUCTION_READINESS.md) and the
[Well-Architected review](../../docs/architecture/gcp-terraform-well-architected-review.md).

## Ownership boundary

Reusable modules live in `modules/`. The separately controlled live configuration
owns every deployable root, including public delegation, plus provider aliases,
remote-state bucket names, tfvars, imports, and promotion evidence. A non-deployable
public-zone composition fixture lives under `modules/dns/tests/fixtures/dns_hub` solely
to exercise the reusable DNS module without creating a second state owner.

The empty `development`, `staging`, and `production` directories are reserved names,
not deployable roots. They must not be materialized until organization, billing,
state, identity, region, IPAM, retention, compliance, SLO, RTO/RPO, and cost
allocation decisions are approved. Guessing those values would create a plausible
but unsafe landing zone.

## Canonical module layers

| Layer | Canonical modules | Composition contract |
|---|---|---|
| Hierarchy/governance | `organization`, `folder_factory`, `project`, `org_policy`, `essential_contacts` | Organization owns tags and additive org IAM; folder/project modules own their respective resources; policy is separate state. |
| Identity/security | `workload_identity`, `kms`, `secret_manager`, `binauthz`, `scc`, `iap_access`, `access_transparency` | Keyless identities and additive IAM only; payloads and authoritative policies are excluded. |
| Network/DNS | `network`, `private_service_access`, `internal_address`, `dns` | IPAM, firewall policy, Shared VPC authority, hybrid connectivity, and PSC require separate approved designs. |
| Runtime/data | `gke`, `cpu_node_pool`, `gpu_node_pool`, `postgres`, `redis`, `pubsub` | Regional private GKE is the execution substrate; workload policy and data-consumer idempotence remain runtime responsibilities. |
| Storage | `storage`, `object_storage`, `bazel_remote_cache`, `nix_binary_cache`, `artifact_registry` | `storage` owns one protected bucket; composition modules select distinct data/cache policies and never duplicate bucket authority. |
| Operations | `log_sink`, `audit_archive`, `monitoring`, `observability` | `log_sink` and `monitoring` own resources; archive/observability compose standardized organization and multi-project views. |
| Build execution | `bazel_remote_execution`, `compute_image`, `workstation` | Composes a CPU pool and keyless executor identity; Kubernetes executor deployment and Bazel policy remain GitOps/Bazel-owned. `compute_image` imports one content-addressed, generation-recorded raw-disk artifact without creating its bucket or publisher. `workstation` consumes only an exact NixOS image self-link, is reachable only through IAP, and holds no signing, publication, or attestation authority. |

`object_storage` does not replace `storage`; `observability` does not replace
`monitoring`; and `audit_archive` does not replace `log_sink`. These higher-level
modules exist to apply a standard portfolio without creating a second owner for the
same resource.

## State and dependency model

Split roots by lifetime, authority, and blast radius, not merely by environment:

```text
seed/bootstrap
  -> organization/hierarchy/policy
     -> audit/security and workload identity
        -> DNS and network/IPAM
           -> environment projects and regional GKE
              -> data services, object/event planes, and build pools
                 -> workload/GitOps configuration
     -> observability, cost, drift, and recovery evidence
```

Each root uses a protected remote GCS backend supplied during `terraform init`, a
unique prefix, version recovery, keyless service-account impersonation, and one
concurrency authority. Keep roots to a few dozen resources where practical. Never
share one state across environments, never commit backend credentials, and never let
a plan identity apply.

Bootstrap is necessarily outside the state it creates. The bootstrap procedure must
be idempotent, keyless, narrowly authorized, recorded, and followed by an import or
explicit ownership boundary. A versioned bucket alone is not a recovery test.

## Module contract

Every materialized module must have:

- `versions.tf`, typed inputs with plan-time safety checks, stable outputs, and a
  README describing failure modes and non-responsibilities;
- additive IAM resources, private defaults, governance labels, bounded capacity and
  retention, and provider/API deletion protection where supported;
- positive and negative mock-provider tests, plus connected qualification before
  production use;
- no secret values, service-account keys, local-exec cloud mutation, provider blocks,
  backend blocks, or environment-specific identifiers;
- an explicit migration plan for breaking addresses or destructive changes.

Root modules pin provider selections in committed lock files. Reusable child modules
declare compatible ranges and do not configure providers. The repository records the
minimum and reviewed Google provider versions in `provider-compatibility.toml`, keeps
canonical three-platform lock fixtures under `locks/`, and qualifies both selections.

## Safe local validation

```bash
nix develop .#ci --command ci/terraform/check.sh all
```

The driver exposes narrower `fmt`, `contracts`, `validate`, `lint`, `security`, `test`,
`docs`, and `compat` phases. It initializes serially, executes bounded workers, preserves
committed locks, and may download checksum-verified provider plugins; it does not access a
backend or cloud API.

Generate documentation only for an intentional interface change:

```bash
nix develop .#ci --command infra/terraform/governance/generate.sh
```

Evaluate JSON derived from one saved plan against an approved environment profile with:

```bash
infra/terraform/policy/check-plan.sh \
  --plan plan.json \
  --profile approved-policy-profile.json \
  --approval exact-expiring-approval.json
```

Omit `--approval` when the plan has no approval-gated action. The repository contains only
synthetic policy profiles. The controlled live pipeline must
authenticate approval provenance and bind apply authorization to the same immutable commit,
binary saved-plan digest, and reviewed JSON evidence.

A production change additionally requires a saved plan and JSON review, policy and
security scans, cost evidence, explicit destructive/IAM/public-access review, a
separate protected apply identity, post-apply drift, and service-level qualification.
No command in this README authorizes an apply.
