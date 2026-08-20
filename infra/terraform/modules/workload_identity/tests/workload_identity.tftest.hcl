# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

mock_provider "google" {}

variables {
  project_id     = "identity-prod-1234"
  project_number = "123456789012"

  pool = {
    pool_id      = "github-actions"
    display_name = "GitHub Actions"
    description  = "Constrained CI federation"
  }

  oidc_providers = {
    github = {
      provider_id       = "github-oidc"
      issuer_uri        = "https://token.actions.githubusercontent.com"
      allowed_audiences = ["https://github.com/mindclade"]
      attribute_mapping = {
        "google.subject"       = "assertion.sub"
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
    api = {
      account_id    = "api-runtime"
      project_roles = ["roles/secretmanager.secretAccessor"]
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

  gke_ksa_bindings = {
    api = {
      service_account_key = "api"
      namespace           = "api"
      ksa_name            = "api"
    }
  }
}

run "keyless_federation_and_gke_contract" {
  command = plan

  assert {
    condition = (
      google_iam_workload_identity_pool.external[0].project == "identity-prod-1234" &&
      google_iam_workload_identity_pool.external[0].workload_identity_pool_id == "github-actions" &&
      google_iam_workload_identity_pool.external[0].deletion_policy == "PREVENT" &&
      google_iam_workload_identity_pool_provider.oidc["github"].deletion_policy == "PREVENT" &&
      google_iam_workload_identity_pool_provider.oidc["github"].attribute_mapping["google.subject"] == "assertion.sub"
    )
    error_message = "The external trust root must preserve its reviewed IDs, mappings, and deletion protection."
  }

  assert {
    condition = (
      length(google_service_account.this) == 2 &&
      google_service_account.this["release"].account_id == "release-publisher" &&
      google_service_account.this["release"].deletion_policy == "PREVENT" &&
      google_project_iam_member.service_account_roles["release/roles/artifactregistry.writer"].role == "roles/artifactregistry.writer"
    )
    error_message = "Workloads must receive dedicated deletion-protected GSAs and additive project member grants."
  }

  assert {
    condition = (
      google_service_account_iam_member.federated["repository_release"].role == "roles/iam.workloadIdentityUser" &&
      google_service_account_iam_member.federated["repository_release"].member == "principalSet://iam.googleapis.com/projects/123456789012/locations/global/workloadIdentityPools/github-actions/attribute.repository/mindclade/mindclade" &&
      google_service_account_iam_member.gke["api"].role == "roles/iam.workloadIdentityUser" &&
      google_service_account_iam_member.gke["api"].member == "serviceAccount:identity-prod-1234.svc.id.goog[api/api]"
    )
    error_message = "External and GKE workloads must use canonical, narrowly generated Workload Identity members."
  }
}

run "gke_only_contract" {
  command = plan

  variables {
    pool                     = null
    oidc_providers           = {}
    federated_principal_sets = {}
  }

  assert {
    condition = (
      length(google_iam_workload_identity_pool.external) == 0 &&
      length(google_iam_workload_identity_pool_provider.oidc) == 0 &&
      google_service_account_iam_member.gke["api"].member == "serviceAccount:identity-prod-1234.svc.id.goog[api/api]"
    )
    error_message = "GKE-only instances must not create an external trust pool."
  }
}

run "rejects_provider_without_pool" {
  command = plan

  variables {
    pool = null
  }

  expect_failures = [var.pool]
}

run "rejects_missing_subject_mapping" {
  command = plan

  variables {
    oidc_providers = {
      github = {
        provider_id       = "github-oidc"
        issuer_uri        = "https://token.actions.githubusercontent.com"
        allowed_audiences = ["https://github.com/mindclade"]
        attribute_mapping = {
          "attribute.repository" = "assertion.repository"
        }
        attribute_condition = "attribute.repository == 'mindclade/mindclade'"
      }
    }
  }

  expect_failures = [var.oidc_providers]
}

run "rejects_missing_custom_attribute_mapping" {
  command = plan

  variables {
    oidc_providers = {
      github = {
        provider_id         = "github-oidc"
        issuer_uri          = "https://token.actions.githubusercontent.com"
        allowed_audiences   = ["https://github.com/mindclade"]
        attribute_mapping   = { "google.subject" = "assertion.sub" }
        attribute_condition = "assertion.repository == 'mindclade/mindclade'"
      }
    }
  }

  expect_failures = [var.oidc_providers]
}

run "rejects_empty_allowed_audiences" {
  command = plan

  variables {
    oidc_providers = {
      github = {
        provider_id       = "github-oidc"
        issuer_uri        = "https://token.actions.githubusercontent.com"
        allowed_audiences = []
        attribute_mapping = {
          "google.subject"       = "assertion.sub"
          "attribute.repository" = "assertion.repository"
        }
        attribute_condition = "attribute.repository == 'mindclade/mindclade'"
      }
    }
  }

  expect_failures = [var.oidc_providers]
}

run "rejects_unconstrained_oidc_condition" {
  command = plan

  variables {
    oidc_providers = {
      github = {
        provider_id       = "github-oidc"
        issuer_uri        = "https://token.actions.githubusercontent.com"
        allowed_audiences = ["https://github.com/mindclade"]
        attribute_mapping = {
          "google.subject"       = "assertion.sub"
          "attribute.repository" = "assertion.repository"
        }
        attribute_condition = "true"
      }
    }
  }

  expect_failures = [var.oidc_providers]
}

run "rejects_basic_workload_role" {
  command = plan

  variables {
    service_accounts = {
      unsafe = {
        account_id    = "unsafe-workload"
        project_roles = ["roles/editor"]
      }
    }
    federated_principal_sets = {}
    gke_ksa_bindings         = {}
  }

  expect_failures = [var.service_accounts]
}

run "rejects_administrator_workload_role" {
  command = plan

  variables {
    service_accounts = {
      unsafe = {
        account_id    = "unsafe-workload"
        project_roles = ["roles/storage.admin"]
      }
    }
    federated_principal_sets = {}
    gke_ksa_bindings         = {}
  }

  expect_failures = [var.service_accounts]
}

run "rejects_wildcard_principal_set" {
  command = plan

  variables {
    federated_principal_sets = {
      unsafe = {
        service_account_key = "release"
        provider_key        = "github"
        attribute           = "repository"
        value               = "mindclade/*"
      }
    }
  }

  expect_failures = [var.federated_principal_sets]
}

run "rejects_unknown_federated_service_account" {
  command = plan

  variables {
    federated_principal_sets = {
      missing = {
        service_account_key = "missing"
        provider_key        = "github"
        attribute           = "repository"
        value               = "mindclade/mindclade"
      }
    }
  }

  expect_failures = [google_service_account_iam_member.federated["missing"]]
}

run "rejects_unknown_oidc_provider" {
  command = plan

  variables {
    federated_principal_sets = {
      missing = {
        service_account_key = "release"
        provider_key        = "missing"
        attribute           = "repository"
        value               = "mindclade/mindclade"
      }
    }
  }

  expect_failures = [google_service_account_iam_member.federated["missing"]]
}

run "rejects_unmapped_principal_set_attribute" {
  command = plan

  variables {
    federated_principal_sets = {
      environment = {
        service_account_key = "release"
        provider_key        = "github"
        attribute           = "environment"
        value               = "production"
      }
    }
  }

  expect_failures = [google_service_account_iam_member.federated["environment"]]
}

run "rejects_invalid_ksa_name" {
  command = plan

  variables {
    gke_ksa_bindings = {
      invalid = {
        service_account_key = "api"
        namespace           = "api"
        ksa_name            = "API_Service"
      }
    }
  }

  expect_failures = [var.gke_ksa_bindings]
}

run "rejects_unknown_gke_service_account" {
  command = plan

  variables {
    gke_ksa_bindings = {
      missing = {
        service_account_key = "missing"
        namespace           = "api"
        ksa_name            = "api"
      }
    }
  }

  expect_failures = [google_service_account_iam_member.gke["missing"]]
}
