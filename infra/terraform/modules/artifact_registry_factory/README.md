# Artifact Registry factory module

Creates a typed, CMEK-protected collection of Docker and language-package repositories while
preserving one Terragrunt state boundary. Docker repositories can require immutable tags and
inherited vulnerability scanning. Cleanup policies always begin in dry-run mode.

The module deliberately reports, but does not apply, the cross-state KMS grant required by the
Artifact Registry service agent.

## Remote (proxy) repositories

A repository may instead be declared with `mode = "REMOTE_REPOSITORY"` and a
`remote_repository_config`. Artifact Registry then serves a read-through cache of a public
upstream under a first-party `pkg.dev` name.

This exists for hosts that have no route to the internet. A private VM whose environment denies
egress by default still reaches `pkg.dev` through the `restricted.googleapis.com` VIP
(`199.36.153.4/30`) that the shared DNS hub already resolves and that the baseline firewall
already permits on 443. A proxy repository is therefore how such a host installs Debian packages
**without** opening a firewall rule, a NAT path, or a proxy allowlist to the public internet.

```hcl
module "workstation_packages" {
  source = "../../modules/artifact_registry_factory"

  project_id                      = "mindclade-development-tools"
  location                        = "us-central1"
  encryption_key                  = "projects/mindclade-seed/locations/us-central1/keyRings/tools/cryptoKeys/artifacts"
  remote_upstream_egress_approved = true

  repositories = {
    debian-bookworm = {
      format      = "APT"
      description = "Read-through cache of the public Debian archive"
      mode        = "REMOTE_REPOSITORY"
      remote_repository_config = {
        public_upstream = "DEBIAN"
        upstream_path   = "debian/dists/bookworm"
      }
    }
  }
}
```

`remote_upstream_egress_approved` defaults to `false` and is a precondition, not a variable
validation, so the failure names the repository that would have started proxying. Declaring a
proxy is a supply-chain decision; it must not arrive as an unremarked line in a `repositories`
map.

### Addressing a proxy from a client

`remote_upstreams[*].client_base_uri` reports the client-facing base. APT and YUM clients address
`https://LOCATION-FORMAT.pkg.dev/projects/PROJECT` and name the repository as the distribution
component of the sources entry. They do **not** use the `PROJECT/REPOSITORY` publication root
that the generic `repositories[*].uri` output renders — that output is correct for Docker and the
language formats and is deliberately unchanged, because its value expression is part of the
governed public interface. Confirm the exact `sources.list` line against Google's remote
repository client documentation before baking it into an image.

### Accepted upstreams

The upstream is restricted to the closed enumerations Artifact Registry publishes for each
format, taken from the pinned provider schema (google 7.45.0):

| `format` | `public_upstream` | `upstream_path` |
| -------- | ----------------- | --------------- |
| `APT`    | `DEBIAN`, `DEBIAN_SNAPSHOT`, `UBUNTU` | required, e.g. `debian/dists/bookworm` |
| `YUM`    | `CENTOS`, `CENTOS_DEBUG`, `CENTOS_STREAM`, `CENTOS_VAULT`, `EPEL`, `ROCKY` | required, e.g. `pub/rocky/9/BaseOS/x86_64/os` |
| `MAVEN`  | `MAVEN_CENTRAL` | must be omitted |
| `NPM`    | `NPMJS` | must be omitted |
| `PYTHON` | `PYPI` | must be omitted |

`upstream_path` accepts only a relative path of alphanumeric-led segments, so a scheme, an
authority, a leading slash, or a `.`/`..` segment cannot retarget the upstream away from the base
named by `public_upstream`.

A `REMOTE_REPOSITORY` rejects `cleanup_policies` and `docker_config`. Both are
`STANDARD_REPOSITORY` settings: a `DELETE` policy on a proxy evicts cached upstream copies rather
than reclaiming our own storage, and the next install silently re-fetches from the public
internet — reinstating the dependency the cache exists to remove. `immutable_tags` is a Docker
publication control and cannot be honoured by a proxy at all.

## What proxying does and does not imply

**It relocates the trust boundary; it does not remove one.** The outbound fetch is made by
Google's infrastructure, not from our VPC. Our egress policy is unchanged and the default-deny
firewall still holds. What changes is that a public archive now reaches our hosts through a
Google-managed control plane we do not configure, under a name that reads as first-party. Treat
every artifact served from a proxy as the untrusted external input it is.

**It is not a pin, a mirror, or an allowlist.** The cache is populated on demand from whatever
the upstream serves at that moment; nothing here fixes a package set, and the repository holds no
reviewed content until something asks for it. `DEBIAN` with `dists/stable` tracks whatever stable
currently is and changes under you across a point release. Where reproducibility matters, use
`DEBIAN_SNAPSHOT` with a dated path so the upstream itself is immutable, and treat a change to
`upstream_path` as the version bump it is.

**It does not verify anything on your behalf.** Artifact Registry does not validate Debian
`Release` signatures for us. The client must still configure a trusted keyring and `Signed-By`,
and must still fail closed on a signature error. A proxy that is trusted because it is on a
`pkg.dev` host is a worse position than the public archive with signature checking on.

**Vulnerability scanning does not cover it.** `enable_vulnerability_scanning` applies inherited
Artifact Analysis to Docker repositories only. It has no effect on an APT, YUM, or language
proxy, and a proxied package is unscanned.

**The saved-plan policy expects a delete policy.** `infra/terraform/policy/terraform_plan.rego`
raises `RETENTION_INSUFFICIENT` for a `google_artifact_registry_repository` whose classification is
governed and which declares no `DELETE` cleanup policy. A proxy declares none by design, so a saved
plan containing one is denied until the reviewed classification profile learns to describe a cache
separately from a publication root. That is a policy change with its own review. Do not add a
cleanup policy here to make a plan pass — it would delete cached upstream artifacts, which is the
behaviour this module rejects.

**Binary Authorization does not apply.** `infra/security/image-policy.yaml` and the `binauthz`
module gate *pods* on an image attestation. A remote repository is not a publication root, holds
nothing anyone signed, and cannot produce an attestation. This is why `DOCKER` remote proxying is
deliberately not offered here even though the provider exposes a `DOCKER_HUB` upstream: a proxied
image would carry no attestation, its upstream tags would remain mutable, and its URI would be
indistinguishable from the attested roots that admission policy is written against. Widening that
enumeration later is an additive change and should be made only alongside an admission story.

**The collection CMEK covers the cached copies** — `encryption_key` is applied uniformly — but the
cross-state grant reported by `required_kms_grant` must already be in place before the first
fetch.

**IAM is still the caller's job.** This module grants nothing. A proxy fetches on behalf of
whoever holds `roles/artifactregistry.reader` on it, so scope that grant to the workload identity
that needs packages rather than to a project-wide principal.

### Deliberate omissions

The provider surface is wider than what this module exposes. Each of the following is withheld on
purpose, and each would be an additive change to reintroduce with its own review:

- `common_repository` / `custom_repository` free-form upstream URIs — an arbitrary host is an
  unreviewable ingress point wearing a `pkg.dev` name. Only the closed enums above are reachable.
- `upstream_credentials` — proxying an authenticated private upstream puts a Secret Manager
  version in this module's contract and extends its blast radius to a third party's access
  control.
- `no_cache` (connector mode) — a pass-through proxy that caches nothing makes every install a
  live internet fetch, removing the one property that makes this arrangement tolerable.
- `disable_upstream_validation` — left at the provider default (`false`) so Google proves the
  upstream resolves at create time. Without it, a typo in `upstream_path` yields a repository
  that exists, plans clean, and returns 404 to every client on first use.
- `mode = "VIRTUAL_REPOSITORY"` — ordered resolution across private and remote upstreams under a
  single endpoint is the configuration that turns a dependency-confusion push upstream into a
  silent substitution for a private package.

<!-- BEGIN_TF_DOCS -->
## Requirements

| Name | Version |
| ---- | ------- |
| <a name="requirement_terraform"></a> [terraform](#requirement\_terraform) | >= 1.9.0, < 2.0.0 |
| <a name="requirement_google"></a> [google](#requirement\_google) | >= 7.41.0, < 8.0.0 |

## Providers

| Name | Version |
| ---- | ------- |
| <a name="provider_google"></a> [google](#provider\_google) | >= 7.41.0, < 8.0.0 |

## Inputs

| Name | Description | Type | Default | Required |
| ---- | ----------- | ---- | ------- | :------: |
| <a name="input_enable_vulnerability_scanning"></a> [enable\_vulnerability\_scanning](#input\_enable\_vulnerability\_scanning) | Require inherited Artifact Analysis scanning for Docker repositories. | `bool` | `true` | no |
| <a name="input_encryption_key"></a> [encryption\_key](#input\_encryption\_key) | CMEK used by every repository; its owning state must grant the Artifact Registry service agent. | `string` | n/a | yes |
| <a name="input_labels"></a> [labels](#input\_labels) | Governance labels applied to every repository. | `map(string)` | `{}` | no |
| <a name="input_location"></a> [location](#input\_location) | Artifact Registry location shared by the collection. | `string` | n/a | yes |
| <a name="input_project_id"></a> [project\_id](#input\_project\_id) | Project that owns the repository collection. | `string` | n/a | yes |
| <a name="input_remote_upstream_egress_approved"></a> [remote\_upstream\_egress\_approved](#input\_remote\_upstream\_egress\_approved) | Explicit acknowledgement required before any repository in the collection may proxy a public upstream. | `bool` | `false` | no |
| <a name="input_repositories"></a> [repositories](#input\_repositories) | Repositories keyed by stable Terraform identity. | <pre>map(object({<br/>    format      = string<br/>    description = string<br/>    mode        = optional(string, "STANDARD_REPOSITORY")<br/>    docker_config = optional(object({<br/>      immutable_tags = optional(bool, true)<br/>    }))<br/>    remote_repository_config = optional(object({<br/>      description     = optional(string)<br/>      public_upstream = string<br/>      upstream_path   = optional(string)<br/>    }))<br/>    cleanup_policies = optional(map(object({<br/>      action               = string<br/>      condition_state      = optional(string)<br/>      older_than           = optional(string)<br/>      most_recent_versions = optional(number)<br/>    })), {})<br/>  }))</pre> | n/a | yes |

## Outputs

| Name | Description |
| ---- | ----------- |
| <a name="output_remote_upstreams"></a> [remote\_upstreams](#output\_remote\_upstreams) | Pinned public upstream and client-facing base URI for each remote proxy repository; empty when the collection proxies nothing. |
| <a name="output_repositories"></a> [repositories](#output\_repositories) | Repository names and immutable publication roots keyed by caller identity. |
| <a name="output_required_kms_grant"></a> [required\_kms\_grant](#output\_required\_kms\_grant) | Exact cross-state grant the KMS owner must apply after resolving the project number. |
<!-- END_TF_DOCS -->
