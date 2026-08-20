# mindclade-internal-monorepo production blueprint

**Repository class:** `source-monorepo`<br>
**Visibility:** `internal`<br>
**Default branch:** `main`

## Authoritative responsibilities

- product, model, training, data, serving, platform, and SDK source
- Bazel build graph and pinned Nix toolchains
- container and deployable package definitions
- environment-neutral Terraform modules and Kubernetes package templates
- Buildkite build, qualification, and release pipelines
- immutable artifacts, SBOMs, provenance, and qualification evidence

## Explicit exclusions

- production environment image references
- live Google Cloud desired state
- Kubernetes environment overlays and Argo CD production configuration
- GitHub organization governance
- cluster credentials and plaintext secrets

## Operating invariant

The monorepo produces immutable artifacts. `infrastructure-live` consumes released Terraform
modules and owns live cloud state. `gitops` consumes immutable packages and image digests and owns
all in-cluster desired state and environment promotion. GitHub Actions supplies lightweight
repository controls; Buildkite owns heavy builds and qualification.

The canonical cross-repository authority contract is
`mindclade/.github/docs/MINDCLADE_ENTERPRISE_PLATFORM_FOUNDATION_BLUEPRINT.md`.
