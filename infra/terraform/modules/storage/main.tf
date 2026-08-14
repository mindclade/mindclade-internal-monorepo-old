locals {
  baseline_labels = {
    "data-classification" = var.data_classification
    environment           = var.environment
    "managed-by"          = "terraform"
    owner                 = var.owner
  }

  bucket_iam = merge(
    { for member in var.object_viewers : "roles/storage.objectViewer:${member}" => {
      role   = "roles/storage.objectViewer"
      member = member
    } },
    { for member in var.object_creators : "roles/storage.objectCreator:${member}" => {
      role   = "roles/storage.objectCreator"
      member = member
    } },
    { for member in var.object_admins : "roles/storage.objectAdmin:${member}" => {
      role   = "roles/storage.objectAdmin"
      member = member
    } },
  )
}

resource "google_storage_bucket" "this" {
  name                        = var.name
  project                     = var.project_id
  location                    = var.location
  storage_class               = var.storage_class
  force_destroy               = false
  deletion_policy             = "PREVENT"
  uniform_bucket_level_access = true
  public_access_prevention    = "enforced"
  labels                      = merge(var.labels, local.baseline_labels)

  dynamic "encryption" {
    for_each = var.kms_key_name == null ? [] : [var.kms_key_name]
    content {
      default_kms_key_name = encryption.value
    }
  }

  versioning {
    enabled = var.versioning_enabled
  }

  soft_delete_policy {
    retention_duration_seconds = var.soft_delete_retention_days * 86400
  }

  logging {
    log_bucket        = var.access_log_bucket
    log_object_prefix = var.access_log_object_prefix
  }

  dynamic "retention_policy" {
    for_each = var.retention_period_seconds == null ? [] : [var.retention_period_seconds]
    content {
      retention_period = tostring(retention_policy.value)
      is_locked        = var.lock_retention_policy
    }
  }

  dynamic "lifecycle_rule" {
    for_each = var.lifecycle_rules
    content {
      action {
        type          = lifecycle_rule.value.action
        storage_class = lifecycle_rule.value.storage_class
      }
      condition {
        age                        = lifecycle_rule.value.age_days
        days_since_noncurrent_time = lifecycle_rule.value.days_since_noncurrent_time
        matches_prefix             = lifecycle_rule.value.matches_prefix
        matches_suffix             = lifecycle_rule.value.matches_suffix
        num_newer_versions         = lifecycle_rule.value.num_newer_versions
        with_state                 = lifecycle_rule.value.with_state
      }
    }
  }

  lifecycle {
    prevent_destroy = true

    precondition {
      condition = !var.lock_retention_policy || (
        var.retention_period_seconds != null &&
        var.retention_lock_confirmation == "LOCKING A CLOUD STORAGE RETENTION POLICY IS IRREVERSIBLE"
      )
      error_message = "Locking retention is irreversible; set a retention period and the exact retention_lock_confirmation only after security, legal, cost, and recovery approval."
    }

    precondition {
      condition     = var.access_log_bucket != var.name
      error_message = "access_log_bucket must be a separately governed bucket, not the bucket being logged."
    }

    precondition {
      condition = !var.create_only_workload || (
        var.versioning_enabled &&
        length(var.object_creators) > 0 &&
        length(var.object_admins) == 0 &&
        length(setsubtract(var.object_creators, var.object_viewers)) == 0 &&
        alltrue([for rule in var.lifecycle_rules : rule.action == "AbortIncompleteMultipartUpload"])
      )
      error_message = "create_only_workload requires versioning, at least one creator that is also a viewer, no object admins, and no Delete or SetStorageClass lifecycle actions. Clients must additionally use ifGenerationMatch=0."
    }
  }
}

resource "google_storage_bucket_iam_member" "this" {
  for_each = local.bucket_iam

  bucket = google_storage_bucket.this.name
  role   = each.value.role
  member = each.value.member
}
