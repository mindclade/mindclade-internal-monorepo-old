# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

mock_provider "google" {}

run "regional_exact_san_certificate" {
  command = plan

  variables {
    project_id     = "mc-production-platform"
    location       = "us-central1"
    dns_project_id = "mc-common-dns"
    dns_authorizations = {
      api = {
        name         = "api-mindclade-ai"
        domain       = "api.mindclade.ai"
        managed_zone = "mindclade-ai"
      }
      models = {
        name         = "models-mindclade-ai"
        domain       = "models.mindclade.ai"
        managed_zone = "mindclade-ai"
      }
      train = {
        name         = "train-mindclade-ai"
        domain       = "train.mindclade.ai"
        managed_zone = "mindclade-ai"
      }
    }
    certificates = {
      ai = {
        name               = "cert-mindclade-ai"
        domains            = ["api.mindclade.ai", "models.mindclade.ai", "train.mindclade.ai"]
        authorization_keys = ["api", "models", "train"]
      }
    }
  }

  assert {
    condition = (
      google_certificate_manager_dns_authorization.this["api"].location == "us-central1" &&
      google_certificate_manager_dns_authorization.this["api"].type == "PER_PROJECT_RECORD" &&
      length(google_certificate_manager_dns_authorization.this) == 3 &&
      google_certificate_manager_certificate.this["ai"].location == "us-central1"
    )
    error_message = "Gateway certificates and per-project authorizations must remain regional."
  }

  assert {
    condition     = google_certificate_manager_certificate.this["ai"].deletion_policy == "PREVENT"
    error_message = "A Gateway certificate must be protected from provider-side deletion."
  }
}

run "wildcard_certificate_is_rejected" {
  command = plan

  variables {
    project_id     = "mc-development-platform"
    location       = "us-central1"
    dns_project_id = "mc-common-dns"
    dns_authorizations = {
      ai = {
        name         = "mindclade-ai"
        domain       = "dev.mindclade.ai"
        managed_zone = "mindclade-ai"
      }
    }
    certificates = {
      ai = {
        name               = "cert-mindclade-ai"
        domains            = ["*.dev.mindclade.ai"]
        authorization_keys = ["ai"]
      }
    }
  }

  expect_failures = [var.certificates]
}

run "uncovered_domain_is_rejected" {
  command = plan

  variables {
    project_id     = "mc-staging-platform"
    location       = "us-central1"
    dns_project_id = "mc-common-dns"
    dns_authorizations = {
      ai = {
        name         = "mindclade-ai"
        domain       = "staging.mindclade.ai"
        managed_zone = "mindclade-ai"
      }
    }
    certificates = {
      ai = {
        name               = "cert-mindclade-ai"
        domains            = ["docs.staging.mindclade.dev"]
        authorization_keys = ["ai"]
      }
    }
  }

  expect_failures = [google_certificate_manager_certificate.this]
}

run "unknown_authorization_key_is_rejected_cleanly" {
  command = plan

  variables {
    project_id     = "mc-staging-platform"
    location       = "us-central1"
    dns_project_id = "mc-common-dns"
    dns_authorizations = {
      api = {
        name         = "api-staging-mindclade-ai"
        domain       = "api.staging.mindclade.ai"
        managed_zone = "mindclade-ai"
      }
    }
    certificates = {
      ai = {
        name               = "cert-mindclade-ai"
        domains            = ["api.staging.mindclade.ai"]
        authorization_keys = ["missing"]
      }
    }
  }

  expect_failures = [google_certificate_manager_certificate.this]
}
