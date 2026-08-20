# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

output "workload_identity_pool" {
  description = "External workload identity pool details, or null for GKE-only module instances."
  value = var.pool == null ? null : {
    id      = google_iam_workload_identity_pool.external[0].id
    name    = google_iam_workload_identity_pool.external[0].name
    pool_id = google_iam_workload_identity_pool.external[0].workload_identity_pool_id
  }
}

output "oidc_providers" {
  description = "OIDC provider details keyed by the caller-provided stable alias."
  value = {
    for alias, provider in google_iam_workload_identity_pool_provider.oidc : alias => {
      id          = provider.id
      name        = provider.name
      provider_id = provider.workload_identity_pool_provider_id
    }
  }
}

output "service_accounts" {
  description = "Dedicated keyless Google service accounts keyed by stable alias."
  value = {
    for alias, account in google_service_account.this : alias => {
      id        = account.id
      name      = account.name
      email     = account.email
      member    = account.member
      unique_id = account.unique_id
    }
  }
}

output "project_role_grants" {
  description = "Additive project role grants keyed by <service-account-alias>/<role>."
  value = {
    for key, grant in google_project_iam_member.service_account_roles : key => {
      project = grant.project
      role    = grant.role
      member  = grant.member
    }
  }
}

output "federated_principal_sets" {
  description = "External principalSet members authorized to impersonate dedicated service accounts."
  value       = local.federated_members
}

output "gke_ksa_members" {
  description = "Canonical GKE KSA members authorized to impersonate dedicated service accounts."
  value       = local.gke_members
}
