# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

mock_provider "google" {}

run "digest_first_repository_collection" {
  command = plan
  variables {
    project_id     = "mindclade-staging-platform"
    location       = "us-central1"
    encryption_key = "projects/mindclade-seed/locations/us-central1/keyRings/staging/cryptoKeys/storage"
    repositories = {
      containers = {
        format        = "DOCKER"
        description   = "Immutable workload images"
        docker_config = { immutable_tags = true }
        cleanup_policies = {
          delete-untagged = { action = "DELETE", condition_state = "UNTAGGED", older_than = "2592000s" }
          keep-recent     = { action = "KEEP", most_recent_versions = 20 }
        }
      }
      python = { format = "PYTHON", description = "Private Python packages" }
    }
  }
  assert {
    condition     = google_artifact_registry_repository.this["containers"].cleanup_policy_dry_run
    error_message = "Cleanup policies must begin in dry-run mode."
  }
  assert {
    condition = (
      google_artifact_registry_repository.this["containers"].mode == "STANDARD_REPOSITORY" &&
      google_artifact_registry_repository.this["python"].mode == "STANDARD_REPOSITORY" &&
      length(google_artifact_registry_repository.this["python"].remote_repository_config) == 0
    )
    error_message = "Repositories must default to a standard publication root with no upstream."
  }
  assert {
    condition     = length(output.remote_upstreams) == 0
    error_message = "A collection that declares no proxy must report no remote upstream."
  }
}

run "remote_apt_proxy_is_pinned_to_a_closed_public_upstream" {
  command = plan
  variables {
    project_id                      = "mindclade-staging-platform"
    location                        = "us-central1"
    encryption_key                  = "projects/mindclade-seed/locations/us-central1/keyRings/staging/cryptoKeys/storage"
    remote_upstream_egress_approved = true
    repositories = {
      debian-bookworm = {
        format      = "APT"
        description = "Read-through cache of the public Debian archive"
        mode        = "REMOTE_REPOSITORY"
        remote_repository_config = {
          description     = "Debian stable"
          public_upstream = "DEBIAN"
          upstream_path   = "debian/dists/bookworm"
        }
      }
    }
  }
  assert {
    condition = (
      google_artifact_registry_repository.this["debian-bookworm"].format == "APT" &&
      google_artifact_registry_repository.this["debian-bookworm"].mode == "REMOTE_REPOSITORY" &&
      google_artifact_registry_repository.this["debian-bookworm"].deletion_policy == "PREVENT"
    )
    error_message = "A remote APT repository must be declared as a deletion-protected proxy."
  }
  assert {
    condition = (
      google_artifact_registry_repository.this["debian-bookworm"].remote_repository_config[0].apt_repository[0].public_repository[0].repository_base == "DEBIAN" &&
      google_artifact_registry_repository.this["debian-bookworm"].remote_repository_config[0].apt_repository[0].public_repository[0].repository_path == "debian/dists/bookworm"
    )
    error_message = "The APT upstream must be the reviewed public base and path."
  }
  assert {
    condition = (
      length(google_artifact_registry_repository.this["debian-bookworm"].docker_config) == 0 &&
      length(google_artifact_registry_repository.this["debian-bookworm"].vulnerability_scanning_config) == 0 &&
      length(google_artifact_registry_repository.this["debian-bookworm"].cleanup_policies) == 0
    )
    error_message = "A proxy must not carry Docker publication, scanning, or cleanup settings it cannot honour."
  }
  assert {
    condition = (
      output.remote_upstreams["debian-bookworm"].public_upstream == "DEBIAN" &&
      output.remote_upstreams["debian-bookworm"].upstream_path == "debian/dists/bookworm" &&
      output.remote_upstreams["debian-bookworm"].client_base_uri == "https://us-central1-apt.pkg.dev/projects/mindclade-staging-platform"
    )
    error_message = "The remote-upstream contract must report the pinned upstream and the APT client base URI."
  }
}

run "remote_repository_requires_reviewed_egress_approval" {
  command = plan
  variables {
    project_id     = "mindclade-staging-platform"
    location       = "us-central1"
    encryption_key = "projects/mindclade-seed/locations/us-central1/keyRings/staging/cryptoKeys/storage"
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

  expect_failures = [
    google_artifact_registry_repository.this,
  ]
}

run "remote_repository_rejects_standard_only_cleanup_policies" {
  command = plan
  variables {
    project_id                      = "mindclade-staging-platform"
    location                        = "us-central1"
    encryption_key                  = "projects/mindclade-seed/locations/us-central1/keyRings/staging/cryptoKeys/storage"
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
        cleanup_policies = {
          delete-stale = { action = "DELETE", older_than = "2592000s" }
        }
      }
    }
  }

  expect_failures = [
    var.repositories,
  ]
}

run "remote_mode_and_upstream_must_be_declared_together" {
  command = plan
  variables {
    project_id                      = "mindclade-staging-platform"
    location                        = "us-central1"
    encryption_key                  = "projects/mindclade-seed/locations/us-central1/keyRings/staging/cryptoKeys/storage"
    remote_upstream_egress_approved = true
    repositories = {
      python = {
        format      = "PYTHON"
        description = "Standard repository carrying an upstream that would be ignored"
        remote_repository_config = {
          public_upstream = "PYPI"
        }
      }
    }
  }

  expect_failures = [
    var.repositories,
  ]
}

run "virtual_repository_mode_is_rejected" {
  command = plan
  variables {
    project_id                      = "mindclade-staging-platform"
    location                        = "us-central1"
    encryption_key                  = "projects/mindclade-seed/locations/us-central1/keyRings/staging/cryptoKeys/storage"
    remote_upstream_egress_approved = true
    repositories = {
      python = {
        format      = "PYTHON"
        description = "Ordered private-plus-public resolution is not offered"
        mode        = "VIRTUAL_REPOSITORY"
      }
    }
  }

  expect_failures = [
    var.repositories,
  ]
}

run "remote_docker_proxying_is_rejected" {
  command = plan
  variables {
    project_id                      = "mindclade-staging-platform"
    location                        = "us-central1"
    encryption_key                  = "projects/mindclade-seed/locations/us-central1/keyRings/staging/cryptoKeys/storage"
    remote_upstream_egress_approved = true
    repositories = {
      dockerhub = {
        format      = "DOCKER"
        description = "Proxied images cannot carry an attestation"
        mode        = "REMOTE_REPOSITORY"
        remote_repository_config = {
          public_upstream = "DOCKER_HUB"
        }
      }
    }
  }

  expect_failures = [
    var.repositories,
  ]
}

run "remote_upstream_outside_the_closed_enum_is_rejected" {
  command = plan
  variables {
    project_id                      = "mindclade-staging-platform"
    location                        = "us-central1"
    encryption_key                  = "projects/mindclade-seed/locations/us-central1/keyRings/staging/cryptoKeys/storage"
    remote_upstream_egress_approved = true
    repositories = {
      debian-mirror = {
        format      = "APT"
        description = "An arbitrary upstream host is not reachable from this module"
        mode        = "REMOTE_REPOSITORY"
        remote_repository_config = {
          public_upstream = "https://deb.example.invalid"
          upstream_path   = "debian/dists/bookworm"
        }
      }
    }
  }

  expect_failures = [
    var.repositories,
  ]
}

run "apt_upstream_path_must_be_a_relative_repository_path" {
  command = plan
  variables {
    project_id                      = "mindclade-staging-platform"
    location                        = "us-central1"
    encryption_key                  = "projects/mindclade-seed/locations/us-central1/keyRings/staging/cryptoKeys/storage"
    remote_upstream_egress_approved = true
    repositories = {
      debian-bookworm = {
        format      = "APT"
        description = "A traversal segment must not retarget the reviewed base"
        mode        = "REMOTE_REPOSITORY"
        remote_repository_config = {
          public_upstream = "DEBIAN"
          upstream_path   = "debian/../../dists/bookworm"
        }
      }
    }
  }

  expect_failures = [
    var.repositories,
  ]
}

run "apt_upstream_path_is_required" {
  command = plan
  variables {
    project_id                      = "mindclade-staging-platform"
    location                        = "us-central1"
    encryption_key                  = "projects/mindclade-seed/locations/us-central1/keyRings/staging/cryptoKeys/storage"
    remote_upstream_egress_approved = true
    repositories = {
      debian-bookworm = {
        format      = "APT"
        description = "The provider requires repository_path for APT"
        mode        = "REMOTE_REPOSITORY"
        remote_repository_config = {
          public_upstream = "DEBIAN"
        }
      }
    }
  }

  expect_failures = [
    var.repositories,
  ]
}

run "language_remote_repository_must_omit_an_upstream_path" {
  command = plan
  variables {
    project_id                      = "mindclade-staging-platform"
    location                        = "us-central1"
    encryption_key                  = "projects/mindclade-seed/locations/us-central1/keyRings/staging/cryptoKeys/storage"
    remote_upstream_egress_approved = true
    repositories = {
      pypi = {
        format      = "PYTHON"
        description = "PyPI has no repository_path field to honour"
        mode        = "REMOTE_REPOSITORY"
        remote_repository_config = {
          public_upstream = "PYPI"
          upstream_path   = "simple"
        }
      }
    }
  }

  expect_failures = [
    var.repositories,
  ]
}

run "language_remote_repository_uses_the_publication_root_uri" {
  command = plan
  variables {
    project_id                      = "mindclade-staging-platform"
    location                        = "us-central1"
    encryption_key                  = "projects/mindclade-seed/locations/us-central1/keyRings/staging/cryptoKeys/storage"
    remote_upstream_egress_approved = true
    repositories = {
      pypi = {
        format      = "PYTHON"
        description = "Read-through cache of the public Python package index"
        mode        = "REMOTE_REPOSITORY"
        remote_repository_config = {
          public_upstream = "PYPI"
        }
      }
    }
  }

  assert {
    condition     = google_artifact_registry_repository.this["pypi"].remote_repository_config[0].python_repository[0].public_repository == "PYPI"
    error_message = "A remote PYTHON repository must resolve to the PYPI public upstream."
  }
  assert {
    condition     = output.remote_upstreams["pypi"].client_base_uri == "https://us-central1-python.pkg.dev/mindclade-staging-platform/pypi"
    error_message = "Language-format proxies must report the <project>/<repository> publication root."
  }
}
