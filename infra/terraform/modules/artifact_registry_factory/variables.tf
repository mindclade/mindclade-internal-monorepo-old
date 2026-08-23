# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

variable "project_id" {
  description = "Project that owns the repository collection."
  type        = string
}

variable "location" {
  description = "Artifact Registry location shared by the collection."
  type        = string
}

variable "encryption_key" {
  description = "CMEK used by every repository; its owning state must grant the Artifact Registry service agent."
  type        = string
}

variable "remote_upstream_egress_approved" {
  description = "Explicit acknowledgement required before any repository in the collection may proxy a public upstream."
  type        = bool
  default     = false
}

variable "repositories" {
  description = "Repositories keyed by stable Terraform identity."
  type = map(object({
    format      = string
    description = string
    mode        = optional(string, "STANDARD_REPOSITORY")
    docker_config = optional(object({
      immutable_tags = optional(bool, true)
    }))
    remote_repository_config = optional(object({
      description     = optional(string)
      public_upstream = string
      upstream_path   = optional(string)
    }))
    cleanup_policies = optional(map(object({
      action               = string
      condition_state      = optional(string)
      older_than           = optional(string)
      most_recent_versions = optional(number)
    })), {})
  }))

  validation {
    condition = length(var.repositories) > 0 && alltrue([
      for repository_id, repository in var.repositories :
      can(regex("^[a-z][a-z0-9-]{1,61}[a-z0-9]$", repository_id)) &&
      contains(["APT", "DOCKER", "GO", "MAVEN", "NPM", "PYTHON", "YUM"], repository.format) &&
      (repository.format == "DOCKER" || repository.docker_config == null) &&
      alltrue([
        for policy in values(repository.cleanup_policies) :
        contains(["DELETE", "KEEP"], policy.action) &&
        ((policy.condition_state != null || policy.older_than != null) != (policy.most_recent_versions != null))
      ])
    ])
    error_message = "Repositories require stable IDs, supported formats, format-compatible Docker settings, and exactly one cleanup selector per policy."
  }

  # VIRTUAL_REPOSITORY is not accepted. A virtual repository resolves one endpoint across
  # ordered private and remote upstreams, which is the exact configuration that turns a
  # dependency-confusion push upstream into a silent substitution for a private package.
  # Pairing mode with its configuration in a single rule also rejects the two shapes that
  # would otherwise fail late or not at all: a REMOTE_REPOSITORY with no upstream, which
  # Google rejects only at apply time, and a STANDARD_REPOSITORY carrying an upstream that
  # is accepted by Terraform, written nowhere, and reads in review as a working proxy.
  validation {
    condition = alltrue([
      for repository in values(var.repositories) :
      contains(["STANDARD_REPOSITORY", "REMOTE_REPOSITORY"], repository.mode) &&
      (repository.mode == "REMOTE_REPOSITORY") == (repository.remote_repository_config != null)
    ])
    error_message = "Each repository must declare mode STANDARD_REPOSITORY or REMOTE_REPOSITORY, and remote_repository_config exactly when mode is REMOTE_REPOSITORY."
  }

  # remote_repository_config is flattened on purpose, and this rule is why it can be.
  # The provider models one nested block per upstream format (apt_repository, yum_repository,
  # maven_repository, ...); mirroring that shape would let a caller declare an APT upstream on a
  # PYTHON repository and have it silently ignored. Here the repository format selects the block
  # in main.tf and public_upstream is checked against that format's closed enum.
  #
  # Comments are kept out of the type expression itself: the governed interface manifest records
  # the flattened type string verbatim, so prose inside the object would become part of the
  # public contract and every wording change would classify as a breaking type change.
  #
  # The upstream is restricted to the closed enums Artifact Registry publishes per format.
  # The provider also accepts common_repository/custom_repository with a free-form URI; that
  # is deliberately not reachable from this module, because a repository whose upstream is an
  # arbitrary host is an unreviewable ingress point wearing a first-party pkg.dev name.
  # DOCKER is excluded even though DOCKER_HUB is an available enum: a proxied image carries no
  # attestation, its upstream tags stay mutable, and the resulting URI is indistinguishable
  # from the attested publication roots that infra/security/image-policy.yaml governs.
  # GO has no upstream block in the provider schema at all and cannot be proxied.
  validation {
    condition = alltrue([
      for repository in values(var.repositories) :
      contains(lookup({
        APT    = ["DEBIAN", "DEBIAN_SNAPSHOT", "UBUNTU"]
        MAVEN  = ["MAVEN_CENTRAL"]
        NPM    = ["NPMJS"]
        PYTHON = ["PYPI"]
        YUM    = ["CENTOS", "CENTOS_DEBUG", "CENTOS_STREAM", "CENTOS_VAULT", "EPEL", "ROCKY"]
      }, repository.format, []), repository.remote_repository_config.public_upstream)
      if repository.remote_repository_config != null
    ])
    error_message = "A remote repository must name one closed public upstream for its format (APT: DEBIAN, DEBIAN_SNAPSHOT, UBUNTU; YUM: CENTOS, CENTOS_DEBUG, CENTOS_STREAM, CENTOS_VAULT, EPEL, ROCKY; MAVEN: MAVEN_CENTRAL; NPM: NPMJS; PYTHON: PYPI). Proxying DOCKER or GO is not offered by this module."
  }

  # repository_path is required by the provider for APT and YUM and does not exist for the
  # single-enum language formats. The grammar allows only alphanumeric-led path segments, so
  # a scheme, an authority, a leading slash, or a "." / ".." segment cannot be smuggled in to
  # retarget the upstream away from the base named by public_upstream.
  validation {
    condition = alltrue([
      for repository in values(var.repositories) :
      (
        contains(["APT", "YUM"], repository.format)
        ? can(regex("^[A-Za-z0-9][A-Za-z0-9._-]*(/[A-Za-z0-9][A-Za-z0-9._-]*)*$", repository.remote_repository_config.upstream_path))
        : repository.remote_repository_config.upstream_path == null
      )
      if repository.remote_repository_config != null
    ])
    error_message = "APT and YUM remote repositories require upstream_path as a relative path of alphanumeric-led segments (for example debian/dists/stable); other remote formats must omit it."
  }

  # A remote repository holds cached copies of somebody else's artifacts, not releases.
  # A DELETE cleanup policy there evicts the cache rather than reclaiming our own storage,
  # and the next install silently re-fetches from the public internet — which is exactly the
  # dependency the cache exists to remove. immutable_tags is a Docker publication control and
  # has no meaning for a proxy, so accepting it would only advertise a guarantee we cannot make.
  validation {
    condition = alltrue([
      for repository in values(var.repositories) :
      length(repository.cleanup_policies) == 0 && repository.docker_config == null
      if repository.mode == "REMOTE_REPOSITORY"
    ])
    error_message = "A REMOTE_REPOSITORY caches third-party artifacts; cleanup_policies and docker_config are STANDARD_REPOSITORY settings and must be omitted."
  }
}

variable "enable_vulnerability_scanning" {
  description = "Require inherited Artifact Analysis scanning for Docker repositories."
  type        = bool
  default     = true
}

variable "labels" {
  description = "Governance labels applied to every repository."
  type        = map(string)
  default     = {}
}
