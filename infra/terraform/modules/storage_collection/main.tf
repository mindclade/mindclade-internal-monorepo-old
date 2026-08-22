# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

locals {
  sole_bucket_name = length(var.buckets) == 1 ? values(var.buckets)[0].name : null
}

resource "google_storage_bucket" "this" {
  for_each = var.buckets

  project                     = var.project_id
  name                        = each.value.name
  location                    = coalesce(each.value.location, var.location)
  force_destroy               = false
  deletion_policy             = "PREVENT"
  uniform_bucket_level_access = var.uniform_bucket_level_access
  public_access_prevention    = var.public_access_prevention
  labels                      = merge(var.labels, { managed-by = "terraform" })

  encryption { default_kms_key_name = var.encryption_key }
  versioning { enabled = coalesce(each.value.versioning, var.versioning) }

  dynamic "hierarchical_namespace" {
    for_each = each.value.hierarchical_namespace ? [1] : []
    content { enabled = true }
  }

  dynamic "soft_delete_policy" {
    for_each = coalesce(each.value.soft_delete_retention_seconds, var.soft_delete_retention_seconds) == 0 ? [] : [coalesce(each.value.soft_delete_retention_seconds, var.soft_delete_retention_seconds)]
    content { retention_duration_seconds = soft_delete_policy.value }
  }

  dynamic "retention_policy" {
    for_each = each.value.retention_days == null ? [] : [each.value.retention_days]
    content {
      retention_period = tostring(retention_policy.value * 86400)
      is_locked        = true
    }
  }

  dynamic "lifecycle_rule" {
    for_each = each.value.lifecycle_rules == null ? var.lifecycle_rules : each.value.lifecycle_rules
    content {
      action {
        type          = lifecycle_rule.value.action.type
        storage_class = lifecycle_rule.value.action.storage_class
      }
      condition {
        age                        = lifecycle_rule.value.condition.age
        days_since_noncurrent_time = lifecycle_rule.value.condition.days_since_noncurrent_time
        matches_storage_class      = lifecycle_rule.value.condition.matches_storage_class
        num_newer_versions         = lifecycle_rule.value.condition.num_newer_versions
        with_state                 = lifecycle_rule.value.condition.with_state
      }
    }
  }

  lifecycle {
    prevent_destroy = true
    precondition {
      condition     = each.value.retention_days == null || var.retention_lock_confirmation == "LOCKING A CLOUD STORAGE RETENTION POLICY IS IRREVERSIBLE"
      error_message = "Locked retention requires the exact irreversible-action confirmation."
    }
  }
}

# Additive, bucket-scoped grants only. This module deliberately does not expose authoritative
# IAM bindings or policies, so a holdout evaluator cannot replace unrelated bucket access.
resource "google_storage_bucket_iam_member" "reader" {
  for_each = var.bucket_iam_members

  bucket = google_storage_bucket.this[each.value.bucket_key].name
  role   = each.value.role
  member = each.value.member
}

resource "google_iam_deny_policy" "this" {
  for_each = var.deny_policies

  parent          = urlencode("cloudresourcemanager.googleapis.com/projects/${var.project_id}")
  name            = each.key
  display_name    = each.value.display_name
  deletion_policy = "PREVENT"

  dynamic "rules" {
    for_each = each.value.rules
    content {
      description = "Restricted to gs://${local.sole_bucket_name}; managed by storage_collection."
      deny_rule {
        denied_principals    = rules.value.denied_principals
        denied_permissions   = rules.value.denied_permissions
        exception_principals = rules.value.exception_principals
        denial_condition {
          title       = "Only the governed bucket"
          description = "Prevent a project-scoped deny policy from affecting sibling buckets."
          expression  = "resource.name.startsWith('projects/_/buckets/${local.sole_bucket_name}/')"
        }
      }
    }
  }
}
