# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

locals {
  profile_machine_types = {
    GENERAL_PURPOSE = "n2-standard-8"
    HIGH_MEMORY     = "n2-highmem-8"
  }

  machine_type = coalesce(var.machine_type, local.profile_machine_types[var.profile])

  baseline_resource_labels = {
    "data-classification" = var.data_classification
    environment           = var.environment
    "managed-by"          = "terraform"
    owner                 = var.owner
    profile               = lower(replace(var.profile, "_", "-"))
    system                = "mindclade"
  }

  resource_labels = merge(var.resource_labels, local.baseline_resource_labels)

  node_labels = merge(var.node_labels, {
    "mindclade.dev/capacity-type"    = lower(replace(var.capacity_type, "_", "-"))
    "mindclade.dev/node-pool"        = "cpu"
    "mindclade.dev/workload-profile" = lower(replace(var.profile, "_", "-"))
  })

  profile_taints = var.profile == "HIGH_MEMORY" ? [{
    effect = "NO_SCHEDULE"
    key    = "scheduling.mindclade.dev/high-memory"
    value  = "true"
  }] : []

  capacity_taints = var.capacity_type == "SPOT" ? [{
    effect = "NO_SCHEDULE"
    key    = "scheduling.mindclade.dev/spot"
    value  = "true"
  }] : []

  node_taints = concat(local.profile_taints, local.capacity_taints, var.additional_taints)

  required_node_service_account_project_roles = toset([
    "roles/container.defaultNodeServiceAccount",
  ])
}

resource "google_service_account" "nodes" {
  project         = var.project_id
  account_id      = var.service_account_id
  display_name    = coalesce(var.service_account_display_name, "GKE ${var.name} nodes")
  description     = "Dedicated keyless VM identity for the ${var.name} GKE node pool."
  disabled        = false
  deletion_policy = "PREVENT"

  lifecycle {
    prevent_destroy = true
  }
}

resource "google_project_iam_member" "nodes" {
  for_each = local.required_node_service_account_project_roles

  project = var.project_id
  role    = each.value
  member  = "serviceAccount:${google_service_account.nodes.email}"
}

resource "google_container_node_pool" "this" {
  project        = var.project_id
  name           = var.name
  location       = var.region
  cluster        = var.cluster_name
  node_locations = sort(tolist(var.node_locations))

  deletion_policy           = "PREVENT"
  ignore_node_count_changes = true
  max_pods_per_node         = var.max_pods_per_node

  autoscaling {
    total_min_node_count = var.total_min_nodes
    total_max_node_count = var.total_max_nodes
    location_policy      = var.capacity_type == "SPOT" ? "ANY" : "BALANCED"
  }

  management {
    auto_repair  = true
    auto_upgrade = true
  }

  upgrade_settings {
    max_surge       = var.upgrade_max_surge
    max_unavailable = var.upgrade_max_unavailable
    strategy        = "SURGE"
  }

  node_drain_config {
    grace_termination_duration            = var.node_drain_grace_period
    pdb_timeout_duration                  = var.node_drain_pdb_timeout
    respect_pdb_during_node_pool_deletion = true
  }

  network_config {
    enable_private_nodes = true
    pod_range            = var.pod_secondary_range_name
  }

  node_config {
    image_type      = "COS_CONTAINERD"
    machine_type    = local.machine_type
    service_account = google_service_account.nodes.email
    spot            = var.capacity_type == "SPOT"

    oauth_scopes = [
      "https://www.googleapis.com/auth/cloud-platform",
    ]

    labels          = local.node_labels
    resource_labels = local.resource_labels

    metadata = {
      disable-legacy-endpoints = "true"
    }

    boot_disk {
      disk_type = var.boot_disk_type
      size_gb   = var.boot_disk_size_gb
    }

    boot_disk_kms_key = var.boot_disk_kms_key

    shielded_instance_config {
      enable_integrity_monitoring = true
      enable_secure_boot          = true
    }

    workload_metadata_config {
      mode = "GKE_METADATA"
    }

    kubelet_config {
      cpu_cfs_quota                          = true
      insecure_kubelet_readonly_port_enabled = "FALSE"
      pod_pids_limit                         = var.pod_pids_limit
    }

    linux_node_config {
      cgroup_mode = "CGROUP_MODE_V2"
    }

    dynamic "taint" {
      for_each = local.node_taints

      content {
        effect = taint.value.effect
        key    = taint.value.key
        value  = taint.value.value
      }
    }
  }

  depends_on = [google_project_iam_member.nodes]

  lifecycle {
    prevent_destroy = true

    precondition {
      condition     = alltrue([for zone in var.node_locations : startswith(zone, "${var.region}-")])
      error_message = "Every node location must be a zone in region."
    }

    precondition {
      condition     = var.total_min_nodes <= var.total_max_nodes
      error_message = "total_min_nodes must not exceed total_max_nodes."
    }

    precondition {
      condition = (
        (var.capacity_type == "SPOT" && var.spot_approval == "I ACCEPT EVICTION AND CAPACITY-LOSS RISK") ||
        (var.capacity_type == "ON_DEMAND" && var.spot_approval == null)
      )
      error_message = "SPOT requires the exact spot_approval acknowledgement; ON_DEMAND must leave spot_approval null."
    }

    precondition {
      condition     = var.capacity_type != "SPOT" || var.total_min_nodes == 0
      error_message = "Spot pools must have total_min_nodes set to zero so reliable baseline capacity is not assumed."
    }

    precondition {
      condition     = var.profile != "HIGH_MEMORY" || strcontains(local.machine_type, "-highmem-")
      error_message = "HIGH_MEMORY requires a high-memory machine type containing -highmem-."
    }

    precondition {
      condition     = var.profile != "GENERAL_PURPOSE" || !strcontains(local.machine_type, "-highmem-")
      error_message = "GENERAL_PURPOSE must not select a high-memory machine type."
    }

    precondition {
      condition     = var.upgrade_max_surge + var.upgrade_max_unavailable > 0
      error_message = "At least one of upgrade_max_surge or upgrade_max_unavailable must be greater than zero."
    }

    precondition {
      condition     = length(distinct([for taint in local.node_taints : taint.key])) == length(local.node_taints)
      error_message = "Taint keys must be unique; the high-memory and Spot isolation keys are module-managed."
    }
  }

  timeouts {
    create = "60m"
    update = "60m"
    delete = "60m"
  }
}
