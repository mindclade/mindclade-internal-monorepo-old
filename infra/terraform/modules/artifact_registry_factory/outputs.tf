# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

output "repositories" {
  description = "Repository names and immutable publication roots keyed by caller identity."
  value = {
    for key, repository in google_artifact_registry_repository.this : key => {
      id   = repository.repository_id
      name = repository.name
      uri  = "${var.location}-${repository.format == "DOCKER" ? "docker" : lower(repository.format)}.pkg.dev/${var.project_id}/${repository.repository_id}"
    }
  }
}

# Added rather than folded into `repositories` because that output's value expression is part
# of the governed public interface and changing it is a breaking change. It is also the wrong
# shape for OS packages: `uri` renders the <project>/<repository> publication root that Docker
# and the language formats use, while APT and YUM clients address <location>-<format>.pkg.dev
# /projects/<project> and name the repository as the distribution component instead.
output "remote_upstreams" {
  description = "Pinned public upstream and client-facing base URI for each remote proxy repository; empty when the collection proxies nothing."
  value = {
    for key, repository in var.repositories : key => {
      format          = repository.format
      public_upstream = repository.remote_repository_config.public_upstream
      upstream_path   = repository.remote_repository_config.upstream_path
      client_base_uri = (
        contains(["APT", "YUM"], repository.format)
        ? "https://${var.location}-${lower(repository.format)}.pkg.dev/projects/${var.project_id}"
        : "https://${var.location}-${lower(repository.format)}.pkg.dev/${var.project_id}/${key}"
      )
    } if repository.mode == "REMOTE_REPOSITORY"
  }
}

output "required_kms_grant" {
  description = "Exact cross-state grant the KMS owner must apply after resolving the project number."
  value = {
    crypto_key             = var.encryption_key
    service_agent_template = "service-PROJECT_NUMBER@gcp-sa-artifactregistry.iam.gserviceaccount.com"
    role                   = "roles/cloudkms.cryptoKeyEncrypterDecrypter"
  }
}
