# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

locals {
  pool_id = try(var.pool.pool_id, "missing-pool")

  project_role_grants = {
    for grant in flatten([
      for service_account_key, account in var.service_accounts : [
        for role in account.project_roles : {
          key                 = "${service_account_key}/${role}"
          service_account_key = service_account_key
          role                = role
        }
      ]
    ]) : grant.key => grant
  }

  federated_members = {
    for alias, grant in var.federated_principal_sets : alias => format(
      "principalSet://iam.googleapis.com/projects/%s/locations/global/workloadIdentityPools/%s/attribute.%s/%s",
      var.project_number,
      local.pool_id,
      grant.attribute,
      grant.value,
    )
  }

  gke_members = {
    for alias, binding in var.gke_ksa_bindings : alias => format(
      "serviceAccount:%s.svc.id.goog[%s/%s]",
      coalesce(binding.gke_project_id, var.project_id),
      binding.namespace,
      binding.ksa_name,
    )
  }
}

# A pool and its providers are long-lived trust roots. Native deletion policies
# and lifecycle protection require an intentional, reviewed two-step retirement.
resource "google_iam_workload_identity_pool" "external" {
  count = var.pool == null ? 0 : 1

  project                   = var.project_id
  workload_identity_pool_id = var.pool.pool_id
  display_name              = var.pool.display_name
  description               = var.pool.description
  disabled                  = var.pool.disabled
  deletion_policy           = "PREVENT"

  lifecycle {
    prevent_destroy = true
  }
}

resource "google_iam_workload_identity_pool_provider" "oidc" {
  for_each = var.pool == null ? {} : var.oidc_providers

  project                            = var.project_id
  workload_identity_pool_id          = try(google_iam_workload_identity_pool.external[0].workload_identity_pool_id, "missing-pool")
  workload_identity_pool_provider_id = each.value.provider_id
  display_name                       = each.value.display_name
  description                        = each.value.description
  disabled                           = each.value.disabled
  attribute_mapping                  = each.value.attribute_mapping
  attribute_condition                = each.value.attribute_condition
  deletion_policy                    = "PREVENT"

  oidc {
    issuer_uri        = each.value.issuer_uri
    allowed_audiences = each.value.allowed_audiences
  }

  lifecycle {
    prevent_destroy = true

    precondition {
      condition     = var.pool != null
      error_message = "pool must be configured when oidc_providers is non-empty."
    }
  }
}

# These accounts are created by this module so every workload receives a
# dedicated, keyless identity. No google_service_account_key resources exist.
resource "google_service_account" "this" {
  for_each = var.service_accounts

  project         = var.project_id
  account_id      = each.value.account_id
  display_name    = each.value.display_name
  description     = each.value.description
  disabled        = each.value.disabled
  deletion_policy = "PREVENT"

  lifecycle {
    prevent_destroy = true
  }
}

# Project permissions are non-authoritative member grants. Each grant names only
# the dedicated service account generated above.
resource "google_project_iam_member" "service_account_roles" {
  for_each = local.project_role_grants

  project = var.project_id
  role    = each.value.role
  member  = google_service_account.this[each.value.service_account_key].member
}

# External identities may impersonate only an explicitly referenced dedicated
# service account and only through a constrained, mapped pool attribute.
resource "google_service_account_iam_member" "federated" {
  for_each = var.pool == null ? {} : var.federated_principal_sets

  service_account_id = try(google_service_account.this[each.value.service_account_key].name, "projects/${var.project_id}/serviceAccounts/invalid@${var.project_id}.iam.gserviceaccount.com")
  role               = "roles/iam.workloadIdentityUser"
  member             = local.federated_members[each.key]

  lifecycle {
    precondition {
      condition     = var.pool != null
      error_message = "pool must be configured when federated_principal_sets is non-empty."
    }

    precondition {
      condition     = contains(keys(var.service_accounts), each.value.service_account_key)
      error_message = "federated_principal_sets[${each.key}].service_account_key must reference a service account declared in service_accounts."
    }

    precondition {
      condition     = contains(keys(var.oidc_providers), each.value.provider_key)
      error_message = "federated_principal_sets[${each.key}].provider_key must reference a provider declared in oidc_providers."
    }

    precondition {
      condition = try(
        contains(keys(var.oidc_providers[each.value.provider_key].attribute_mapping), "attribute.${each.value.attribute}"),
        false,
      )
      error_message = "The referenced OIDC provider must map attribute.${each.value.attribute} before it can be used by federated_principal_sets[${each.key}]."
    }
  }
}

# GKE Workload Identity uses the canonical KSA member syntax and the same
# additive, narrowly scoped service-account IAM member resource.
resource "google_service_account_iam_member" "gke" {
  for_each = var.gke_ksa_bindings

  service_account_id = try(google_service_account.this[each.value.service_account_key].name, "projects/${var.project_id}/serviceAccounts/invalid@${var.project_id}.iam.gserviceaccount.com")
  role               = "roles/iam.workloadIdentityUser"
  member             = local.gke_members[each.key]

  lifecycle {
    precondition {
      condition     = contains(keys(var.service_accounts), each.value.service_account_key)
      error_message = "gke_ksa_bindings[${each.key}].service_account_key must reference a service account declared in service_accounts."
    }
  }
}
