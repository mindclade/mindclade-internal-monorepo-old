# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

locals {
  gpu_profiles = {
    gke-h100-a3-megagpu-8g = {
      accelerator_count = 8
      accelerator_type  = "nvidia-h100-mega-80gb"
      fabric            = "GPUDirect-TCPXO"
      machine_type      = "a3-megagpu-8g"
    }
    gke-h200-a3-ultragpu-8g = {
      accelerator_count = 8
      accelerator_type  = "nvidia-h200-141gb"
      fabric            = "GPUDirect-RDMA"
      machine_type      = "a3-ultragpu-8g"
    }
  }

  selected_profile = local.gpu_profiles[var.profile]
  boot_disk_type   = var.boot_disk_kms_key == null ? "hyperdisk-balanced" : "pd-ssd"

  baseline_resource_labels = {
    data-classification = var.data_classification
    environment         = var.environment
    gpu-profile         = var.profile
    managed-by          = "terraform"
    owner               = var.owner
    system              = "mindclade"
  }

  resource_labels = merge(var.resource_labels, local.baseline_resource_labels)

  node_labels = merge(var.node_labels, {
    "mindclade.dev/gpu-profile" = var.profile
    "mindclade.dev/node-pool"   = "gpu"
  })

  node_taints = concat(
    [{
      effect = "NO_SCHEDULE"
      key    = "nvidia.com/gpu"
      value  = "present"
    }],
    var.additional_taints,
  )
}

resource "google_container_node_pool" "this" {
  #checkov:skip=CKV_GCP_9:Auto-repair is enabled for ordinary pools and intentionally disabled only for provider-constrained Flex Start/queued capacity; Terraform tests cover the mode contract.
  project        = var.project_id
  name           = var.name
  location       = var.region
  cluster        = var.cluster_name
  node_locations = [var.zone]

  deletion_policy           = "PREVENT"
  ignore_node_count_changes = true
  max_pods_per_node         = var.max_pods_per_node

  autoscaling {
    total_min_node_count = var.total_min_nodes
    total_max_node_count = var.total_max_nodes
    location_policy      = "ANY"
  }

  management {
    auto_repair  = !contains(["FLEX_START", "QUEUED_PROVISIONING"], var.capacity_mode)
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
    accelerator_network_profile = "auto"
    enable_private_nodes        = true
    pod_range                   = var.pod_secondary_range_name
  }

  dynamic "placement_policy" {
    for_each = var.enable_compact_placement ? [1] : []

    content {
      type = "COMPACT"
    }
  }

  dynamic "queued_provisioning" {
    for_each = var.capacity_mode == "QUEUED_PROVISIONING" ? [1] : []

    content {
      enabled = true
    }
  }

  node_config {
    image_type      = "COS_CONTAINERD"
    machine_type    = local.selected_profile.machine_type
    service_account = var.node_service_account_email

    boot_disk_kms_key = var.boot_disk_kms_key
    flex_start        = contains(["FLEX_START", "QUEUED_PROVISIONING"], var.capacity_mode)
    max_run_duration  = contains(["FLEX_START", "QUEUED_PROVISIONING"], var.capacity_mode) ? var.max_run_duration : null
    spot              = var.capacity_mode == "SPOT"

    oauth_scopes = [
      "https://www.googleapis.com/auth/cloud-platform",
    ]

    labels          = local.node_labels
    resource_labels = local.resource_labels

    metadata = {
      disable-legacy-endpoints = "true"
    }

    boot_disk {
      disk_type = local.boot_disk_type
      size_gb   = var.boot_disk_size_gb
    }

    guest_accelerator {
      count = local.selected_profile.accelerator_count
      type  = local.selected_profile.accelerator_type

      gpu_driver_installation_config {
        gpu_driver_version = var.gpu_driver_version
      }
    }

    gvnic {
      enabled = true
    }

    gcfs_config {
      enabled = true
    }

    shielded_instance_config {
      enable_integrity_monitoring = true
      enable_secure_boot          = true
    }

    workload_metadata_config {
      mode = "GKE_METADATA"
    }

    kubelet_config {
      cpu_cfs_quota                               = true
      cpu_manager_policy                          = "static"
      insecure_kubelet_readonly_port_enabled      = "FALSE"
      pod_pids_limit                              = 32768
      shutdown_grace_period_critical_pods_seconds = var.capacity_mode == "SPOT" ? 120 : null
      shutdown_grace_period_seconds               = var.capacity_mode == "SPOT" ? 300 : null

      topology_manager {
        policy = "restricted"
        scope  = "pod"
      }
    }

    linux_node_config {
      cgroup_mode                  = "CGROUP_MODE_V2"
      transparent_hugepage_enabled = "TRANSPARENT_HUGEPAGE_ENABLED_MADVISE"
    }

    dynamic "reservation_affinity" {
      for_each = var.capacity_mode == "RESERVATION" ? [{
        consume_reservation_type = "SPECIFIC_RESERVATION"
        key                      = "compute.googleapis.com/reservation-name"
        values                   = [var.reservation_name]
        }] : contains(["FLEX_START", "QUEUED_PROVISIONING"], var.capacity_mode) ? [{
        consume_reservation_type = "NO_RESERVATION"
        key                      = null
        values                   = null
      }] : []

      content {
        consume_reservation_type = reservation_affinity.value.consume_reservation_type
        key                      = reservation_affinity.value.key
        values                   = reservation_affinity.value.values
      }
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

  lifecycle {
    prevent_destroy = true

    precondition {
      condition     = startswith(var.zone, "${var.region}-")
      error_message = "zone must belong to region."
    }

    precondition {
      condition     = var.total_min_nodes <= var.total_max_nodes
      error_message = "total_min_nodes must not exceed total_max_nodes."
    }

    precondition {
      condition     = !contains(["FLEX_START", "QUEUED_PROVISIONING"], var.capacity_mode) || var.total_min_nodes == 0
      error_message = "Flex Start and queued-provisioning pools must have total_min_nodes set to zero."
    }

    precondition {
      condition     = var.capacity_mode != "FLEX_START" || var.enable_preview_flex_start
      error_message = "Standalone Flex Start is a preview feature and requires enable_preview_flex_start=true after an explicit environment review."
    }

    precondition {
      condition     = var.profile != "gke-h200-a3-ultragpu-8g" || var.capacity_mode != "ON_DEMAND"
      error_message = "The A3 Ultra H200 profile does not support the Standard on-demand provisioning model; select Spot, Flex Start, queued provisioning, or an approved reservation."
    }

    precondition {
      condition     = !contains(["FLEX_START", "QUEUED_PROVISIONING"], var.capacity_mode) || !var.enable_compact_placement
      error_message = "Flex Start and queued provisioning are incompatible with this module's MIG compact placement policy."
    }

    precondition {
      condition = (
        (var.capacity_mode == "RESERVATION" && var.reservation_name != null) ||
        (var.capacity_mode != "RESERVATION" && var.reservation_name == null)
      )
      error_message = "reservation_name is required only when capacity_mode is RESERVATION."
    }

    precondition {
      condition     = var.upgrade_max_surge + var.upgrade_max_unavailable > 0
      error_message = "At least one of upgrade_max_surge or upgrade_max_unavailable must be greater than zero."
    }

    precondition {
      condition     = var.boot_disk_kms_key == null || var.profile == "gke-h100-a3-megagpu-8g"
      error_message = "Boot-disk CMEK is qualified only for the H100 profile with pd-ssd; GKE does not support it with the H200 profile's required Hyperdisk boot disk."
    }

    precondition {
      condition     = length(distinct([for taint in local.node_taints : taint.key])) == length(local.node_taints)
      error_message = "Taint keys must be unique; nvidia.com/gpu is managed by this module."
    }
  }

  timeouts {
    create = "120m"
    update = "120m"
    delete = "60m"
  }
}
