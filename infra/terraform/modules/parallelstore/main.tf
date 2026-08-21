# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

resource "google_parallelstore_instance" "this" {
  for_each = var.parallelstore

  project           = var.project_id
  instance_id       = each.value.name
  location          = each.value.location
  capacity_gib      = tostring(each.value.capacity_gib)
  deployment_type   = each.value.deployment_type
  network           = each.value.network
  reserved_ip_range = each.value.reserved_ip_range
  labels            = merge(var.labels, { managed-by = "terraform" })
  deletion_policy   = "PREVENT"

  lifecycle {
    prevent_destroy = true
  }
  timeouts {
    create = "120m"
    update = "120m"
    delete = "120m"
  }
}
