locals {
  baseline_labels = {
    "data-classification" = var.data_classification
    environment           = var.environment
    "managed-by"          = "terraform"
    owner                 = var.owner
  }

  iam = merge(
    { for member in var.accessor_members : "roles/secretmanager.secretAccessor:${member}" => {
      role   = "roles/secretmanager.secretAccessor"
      member = member
    } },
    { for member in var.version_adder_members : "roles/secretmanager.secretVersionAdder:${member}" => {
      role   = "roles/secretmanager.secretVersionAdder"
      member = member
    } },
    { for member in var.viewer_members : "roles/secretmanager.viewer:${member}" => {
      role   = "roles/secretmanager.viewer"
      member = member
    } },
  )
}

resource "google_secret_manager_secret" "this" {
  project             = var.project_id
  secret_id           = var.secret_id
  labels              = merge(var.labels, local.baseline_labels)
  annotations         = var.annotations
  version_destroy_ttl = "${var.version_destroy_delay_days * 86400}s"
  deletion_protection = true
  deletion_policy     = "PREVENT"

  replication {
    dynamic "auto" {
      for_each = length(var.user_managed_replicas) == 0 ? [1] : []
      content {
        dynamic "customer_managed_encryption" {
          for_each = var.automatic_kms_key_name == null ? [] : [var.automatic_kms_key_name]
          content {
            kms_key_name = customer_managed_encryption.value
          }
        }
      }
    }

    dynamic "user_managed" {
      for_each = length(var.user_managed_replicas) == 0 ? [] : [var.user_managed_replicas]
      content {
        dynamic "replicas" {
          for_each = user_managed.value
          content {
            location = replicas.key
            dynamic "customer_managed_encryption" {
              for_each = replicas.value == null ? [] : [replicas.value]
              content {
                kms_key_name = customer_managed_encryption.value
              }
            }
          }
        }
      }
    }
  }

  dynamic "topics" {
    for_each = var.notification_topics
    content {
      name = topics.value
    }
  }

  dynamic "rotation" {
    for_each = var.rotation == null ? [] : [var.rotation]
    content {
      next_rotation_time = rotation.value.next_rotation_time
      rotation_period    = "${rotation.value.period_days * 86400}s"
    }
  }

  lifecycle {
    prevent_destroy = true

    precondition {
      condition     = length(var.user_managed_replicas) == 0 || var.automatic_kms_key_name == null
      error_message = "automatic_kms_key_name cannot be combined with user_managed_replicas."
    }

    precondition {
      condition     = var.rotation == null || length(var.notification_topics) > 0
      error_message = "A rotation schedule requires at least one notification topic and an external rotation handler."
    }

    precondition {
      condition = var.rotation == null || (
        timecmp(var.rotation.next_rotation_time, timeadd(plantimestamp(), "24h5m")) >= 0 &&
        timecmp(var.rotation.next_rotation_time, timeadd(timestamp(), "5m")) >= 0 &&
        timecmp(var.rotation.next_rotation_time, timeadd(timestamp(), "876000h")) <= 0
      )
      error_message = "next_rotation_time must preserve a five-minute API margin through a saved plan applied within 24 hours, remain at least five minutes after the actual apply time, and be no more than 100 years in the future."
    }

    precondition {
      condition = var.data_classification != "restricted" || (
        length(var.user_managed_replicas) == 0 ?
        var.automatic_kms_key_name != null :
        alltrue([for kms_key_name in values(var.user_managed_replicas) : kms_key_name != null])
      )
      error_message = "Restricted secrets require CMEK for automatic replication or for every user-managed replica."
    }

    precondition {
      condition     = length(setintersection(var.accessor_members, var.version_adder_members)) == 0
      error_message = "Secret payload accessors and version adders must be disjoint identities."
    }
  }
}

resource "google_secret_manager_secret_iam_member" "this" {
  for_each = local.iam

  project   = var.project_id
  secret_id = google_secret_manager_secret.this.secret_id
  role      = each.value.role
  member    = each.value.member
}
