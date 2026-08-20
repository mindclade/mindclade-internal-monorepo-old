# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

mock_provider "google" {}

variables {
  organization_id = "123456789012"

  tag_keys = {
    environment = {
      short_name  = "environment"
      description = "Deployment environment"
      values = {
        production = { short_name = "production" }
        staging    = { short_name = "staging" }
      }
    }
    classification = {
      short_name = "data_classification"
      values = {
        restricted = { short_name = "restricted" }
      }
    }
  }

  tag_bindings = {
    project_environment = {
      parent    = "//cloudresourcemanager.googleapis.com/projects/987654321098"
      tag_value = "environment/production"
    }
  }

  iam_grants = {
    security_reviewers = {
      role   = "roles/iam.securityReviewer"
      member = "group:cloud-security@example.com"
      condition = {
        title      = "time_bounded_review"
        expression = "request.time < timestamp('2027-01-01T00:00:00Z')"
      }
    }
  }
}

run "protected_taxonomy_and_additive_iam_contract" {
  command = plan

  assert {
    condition = (
      length(google_tags_tag_key.this) == 2 &&
      length(google_tags_tag_value.this) == 3 &&
      google_tags_tag_key.this["environment"].parent == "organizations/123456789012" &&
      google_tags_tag_key.this["environment"].deletion_policy == "PREVENT" &&
      google_tags_tag_value.this["environment/production"].deletion_policy == "PREVENT"
    )
    error_message = "The declared organization taxonomy must be materialized with deletion protection."
  }

  assert {
    condition = (
      google_tags_tag_binding.this["project_environment"].parent == "//cloudresourcemanager.googleapis.com/projects/987654321098" &&
      google_tags_tag_binding.this["project_environment"].deletion_policy == "PREVENT"
    )
    error_message = "Tag bindings must retain their exact resource target and deletion protection."
  }

  assert {
    condition = (
      google_organization_iam_member.this["security_reviewers"].org_id == "123456789012" &&
      google_organization_iam_member.this["security_reviewers"].role == "roles/iam.securityReviewer" &&
      google_organization_iam_member.this["security_reviewers"].member == "group:cloud-security@example.com" &&
      google_organization_iam_member.this["security_reviewers"].condition[0].title == "time_bounded_review"
    )
    error_message = "Organization access must remain an additive member grant with its reviewed condition."
  }
}

run "rejects_unknown_tag_value_reference" {
  command = plan

  variables {
    tag_bindings = {
      project_environment = {
        parent    = "//cloudresourcemanager.googleapis.com/projects/987654321098"
        tag_value = "environment/not_declared"
      }
    }
  }

  expect_failures = [google_tags_tag_binding.this["project_environment"]]
}

run "rejects_non_numeric_binding_parent" {
  command = plan

  variables {
    tag_bindings = {
      bad_project = {
        parent    = "//cloudresourcemanager.googleapis.com/projects/project-id"
        tag_value = "environment/production"
      }
    }
  }

  expect_failures = [var.tag_bindings]
}

run "rejects_basic_organization_role" {
  command = plan

  variables {
    iam_grants = {
      unsafe = {
        role   = "roles/owner"
        member = "group:cloud-security@example.com"
      }
    }
  }

  expect_failures = [var.iam_grants]
}

run "rejects_administrator_organization_role" {
  command = plan

  variables {
    iam_grants = {
      unsafe = {
        role   = "roles/resourcemanager.organizationAdmin"
        member = "group:cloud-security@example.com"
      }
    }
  }

  expect_failures = [var.iam_grants]
}

run "rejects_public_principal" {
  command = plan

  variables {
    iam_grants = {
      public = {
        role   = "roles/browser"
        member = "allAuthenticatedUsers"
      }
    }
  }

  expect_failures = [var.iam_grants]
}

run "rejects_direct_user" {
  command = plan

  variables {
    iam_grants = {
      direct_user = {
        role   = "roles/browser"
        member = "user:operator@example.com"
      }
    }
  }

  expect_failures = [var.iam_grants]
}

run "rejects_trivial_iam_condition" {
  command = plan

  variables {
    iam_grants = {
      trivial = {
        role   = "roles/browser"
        member = "group:cloud-security@example.com"
        condition = {
          title      = "always"
          expression = "true"
        }
      }
    }
  }

  expect_failures = [var.iam_grants]
}
