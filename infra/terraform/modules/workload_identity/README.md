# Workload identity module

This module creates dedicated, keyless Google service accounts and wires either
external OIDC workloads or GKE Kubernetes service accounts to them. It can create
one protected external Workload Identity Pool with multiple constrained OIDC
providers, additive project permissions, external `principalSet` impersonation
grants, and GKE KSA impersonation grants.

## Security contract

- The module contains no service-account-key resource or private-key output.
- Every Google service account is created by the module, dedicated to a workload,
  and protected with both provider and Terraform deletion guards.
- Project and service-account IAM use only additive `*_iam_member` resources.
- Basic roles, roles ending in `Admin`, and service-account impersonation roles
  are rejected from workload project permissions.
- Every OIDC provider requires HTTPS issuer metadata, 1-10 explicit non-wildcard
  audiences, `google.subject`, at least one custom `attribute.*` mapping, and a
  non-trivial CEL condition constraining a mapped custom attribute.
- External impersonation uses structured `principalSet` inputs. Wildcard values
  and free-form IAM member strings are not accepted.
- Pool, provider, and service-account deletion is protected. Individual IAM
  grants remain revocable for incident response.

Validation is intentionally conservative and cannot prove CEL semantics or the
permissions inside custom roles. Security reviewers must still inspect each
issuer, audience, mapping, condition, principal-set value, and role.

## External OIDC example

```hcl
module "ci_identity" {
  source = "../../modules/workload_identity"

  project_id     = "identity-prod-1234"
  project_number = "123456789012"

  pool = {
    pool_id      = "github-actions"
    display_name = "GitHub Actions"
  }

  oidc_providers = {
    github = {
      provider_id       = "github-oidc"
      issuer_uri        = "https://token.actions.githubusercontent.com"
      allowed_audiences = ["https://github.com/mindclade"]
      attribute_mapping = {
        "google.subject"      = "assertion.sub"
        "attribute.repository" = "assertion.repository"
      }
      attribute_condition = "attribute.repository == 'mindclade/mindclade'"
    }
  }

  service_accounts = {
    release = {
      account_id    = "release-publisher"
      display_name  = "Release publisher"
      project_roles = ["roles/artifactregistry.writer"]
    }
  }

  federated_principal_sets = {
    repository_release = {
      service_account_key = "release"
      provider_key        = "github"
      attribute           = "repository"
      value               = "mindclade/mindclade"
    }
  }
}
```

The caller must request an OIDC token whose audience exactly matches an entry in
`allowed_audiences`. A `provider_key` validates that the selected attribute is
mapped, but Google principal sets are pool-scoped. If multiple providers share a
pool, coordinate attribute names and values so another provider cannot mint the
same authorized value. Prefer one module/pool per independent trust boundary.

## GKE example

```hcl
module "gke_identity" {
  source = "../../modules/workload_identity"

  project_id     = "runtime-prod-1234"
  project_number = "987654321098"

  service_accounts = {
    api = {
      account_id    = "api-runtime"
      project_roles = ["roles/secretmanager.secretAccessor"]
    }
  }

  gke_ksa_bindings = {
    api = {
      service_account_key = "api"
      namespace           = "api"
      ksa_name            = "api"
    }
  }
}
```

For GKE-only use, leave `pool = null`. This module creates the GSA IAM binding;
the caller remains responsible for enabling Workload Identity Federation on the
cluster and annotating the KSA with
`iam.gke.io/gcp-service-account=<GSA_EMAIL>`. `gke_project_id` defaults to
`project_id` and supports a KSA from another project when explicitly set.

## Inputs and limits

| Input | Contract | Module limit |
| --- | --- | ---: |
| `pool` | One external federation trust boundary, or null | one |
| `oidc_providers` | Constrained OIDC providers | 20 |
| `service_accounts` | Dedicated GSAs, up to 50 project roles each | 100 |
| `federated_principal_sets` | External principalSet-to-GSA grants | 500 |
| `gke_ksa_bindings` | KSA-to-GSA grants | 500 |

These limits bound state and review blast radius rather than mirroring Google
Cloud quotas. Stable map aliases are Terraform state addresses; rename them only
with an explicit caller-side `moved` block.

## Outputs

The module returns pool/provider resource details, dedicated GSA IDs/emails/member
strings, additive project grants, external principalSet URIs, and canonical GKE
KSA member strings. It never returns credentials.

## Operations and exclusions

The caller enables IAM and Service Account Credentials APIs, configures provider
authentication, supplies deployer permissions, configures OIDC claims, and owns
GKE cluster/KSA resources. This module does not create clusters, Kubernetes
objects, service-account keys, secrets, or organization policies.

Pools and providers are soft-deleted by Google and their IDs may remain reserved.
Retirement requires a deliberate two-release sequence: first remove lifecycle
protection and change provider deletion policy, apply that reviewed state change,
then remove the resource. IAM grants can be revoked immediately without changing
deletion protection.

Mock-provider tests in `tests/` cover contracts and graph shape. They do not test
live OIDC exchange, IAM propagation, provider eventual consistency, or external
issuer discovery; run a controlled integration test before production rollout.
