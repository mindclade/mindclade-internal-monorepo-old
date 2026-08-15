# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

locals {
  baseline_labels = {
    "data-classification" = var.data_classification
    environment           = var.environment
    "managed-by"          = "terraform"
    owner                 = var.owner
  }
}

resource "google_redis_instance" "this" {
  project                 = var.project_id
  name                    = var.name
  display_name            = var.display_name
  region                  = var.region
  tier                    = "STANDARD_HA"
  memory_size_gb          = var.memory_size_gb
  redis_version           = var.redis_version
  authorized_network      = var.authorized_network
  connect_mode            = "PRIVATE_SERVICE_ACCESS"
  reserved_ip_range       = var.reserved_ip_range
  location_id             = var.primary_zone
  alternative_location_id = var.alternative_zone
  auth_enabled            = true
  transit_encryption_mode = "SERVER_AUTHENTICATION"
  customer_managed_key    = var.kms_key_name
  redis_configs           = var.redis_configs
  labels                  = merge(var.labels, local.baseline_labels)
  deletion_protection     = true
  deletion_policy         = "PREVENT"

  persistence_config {
    persistence_mode        = "RDB"
    rdb_snapshot_period     = var.rdb_snapshot_period
    rdb_snapshot_start_time = var.rdb_snapshot_start_time
  }

  maintenance_policy {
    description = "Reviewed weekly maintenance window in UTC"
    weekly_maintenance_window {
      day = var.maintenance_day
      start_time {
        hours   = var.maintenance_hour_utc
        minutes = 0
        seconds = 0
        nanos   = 0
      }
    }
  }

  lifecycle {
    prevent_destroy = true

    precondition {
      condition     = var.primary_zone == null || var.alternative_zone == null || var.primary_zone != var.alternative_zone
      error_message = "primary_zone and alternative_zone must be different when both are provided."
    }
  }
}
