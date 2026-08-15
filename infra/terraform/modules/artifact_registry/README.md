# Artifact Registry Docker repository

This module creates one standard private Docker repository with immutable tags,
provider- and Terraform-level deletion guards, inherited vulnerability scanning,
and a bounded **untagged** cleanup candidate. Cleanup starts in dry-run mode and
cannot become destructive until the caller sets `cleanup_activation_approved = true`
in the same reviewed change.

Artifact Registry cannot delete an image that still has an immutable tag. Tagged
release artifacts are therefore intentionally outside this cleanup policy and may
grow without bound. The release governance owner must budget and approve their
retention or rotate whole repositories through a separate, evidence-preserving
workflow. Do not weaken tag immutability or silently describe total repository
retention as bounded.

The module deliberately does not enable APIs, create KMS keys, grant IAM, sign
images, create attestations, or configure admission. Callers must enable
`artifactregistry.googleapis.com`; enable `containerscanning.googleapis.com`
when automatic scanning is required; grant repository-scoped reader/writer
roles to separate workload identities; and verify the effective scanning-state
output. A CMEK, when supplied, must already grant the Artifact Registry service
agent encrypt/decrypt access and use the repository location.

```hcl
module "application_images" {
  source = "../../modules/artifact_registry"

  project_id    = "mindclade-production"
  location      = "us-central1"
  repository_id = "application-images"
  environment   = "production"
  owner         = "cloud-platform"

  untagged_retention_days = 45
  minimum_versions_to_keep = 25
}
```

Before activating cleanup, review dry-run logs for at least one complete cleanup
cycle, prove that rollback digests remain available, and record approval. Deploy
workloads by digest: immutable tags prevent moving or deleting a tag, but they do
not by themselves provide provenance, admission enforcement, or proof that
scanning is active. This module is an infrastructure contract, not deployed or
production-readiness evidence.
