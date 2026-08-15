# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

variable "project_id" {
  description = "GCP project ID that owns the GKE cluster"
  type        = string

  validation {
    condition     = can(regex("^[a-z][a-z0-9-]{4,28}[a-z0-9]$", var.project_id))
    error_message = "project_id must be a valid 6-30 character GCP project ID."
  }
}

variable "cluster_name" {
  description = "Existing regional GKE cluster name"
  type        = string

  validation {
    condition     = can(regex("^[a-z][a-z0-9-]{0,38}[a-z0-9]$", var.cluster_name))
    error_message = "cluster_name must be a valid 2-40 character GKE cluster name."
  }
}

variable "name" {
  description = "GPU node-pool name"
  type        = string

  validation {
    condition     = can(regex("^[a-z][a-z0-9-]{0,38}[a-z0-9]$", var.name))
    error_message = "name must be a valid 2-40 character GKE node-pool name."
  }
}

variable "region" {
  description = "Regional GKE control-plane location"
  type        = string

  validation {
    condition     = can(regex("^[a-z]+(?:-[a-z0-9]+)+[0-9]$", var.region))
    error_message = "region must be a regional location such as us-central1, not a zone."
  }
}

variable "zone" {
  description = "Single approved GPU zone; automated accelerator networking does not support multi-zone node pools"
  type        = string

  validation {
    condition     = can(regex("^[a-z]+(?:-[a-z0-9]+)+[0-9]-[a-z]$", var.zone))
    error_message = "zone must be a zonal location such as us-central1-a."
  }
}

variable "profile" {
  description = "Fixed accelerator profile admitted by the NOVA execution-plan v2 contract"
  type        = string

  validation {
    condition = contains([
      "gke-h100-a3-megagpu-8g",
      "gke-h200-a3-ultragpu-8g",
    ], var.profile)
    error_message = "profile must be gke-h100-a3-megagpu-8g or gke-h200-a3-ultragpu-8g."
  }
}

variable "node_service_account_email" {
  description = "Dedicated user-managed service account for GPU node VMs; grant permissions outside this module"
  type        = string

  validation {
    condition     = can(regex("^[a-z][a-z0-9-]{4,28}[a-z0-9]@[a-z][a-z0-9-]{4,28}[a-z0-9]\\.iam\\.gserviceaccount\\.com$", var.node_service_account_email))
    error_message = "node_service_account_email must be a user-managed GCP service-account email."
  }
}

variable "pod_secondary_range_name" {
  description = "Existing cluster Pod secondary-range name"
  type        = string

  validation {
    condition     = can(regex("^[a-z][a-z0-9-]{0,62}$", var.pod_secondary_range_name))
    error_message = "pod_secondary_range_name must be a valid secondary range name."
  }
}

variable "environment" {
  description = "Environment label applied to node resources"
  type        = string

  validation {
    condition     = can(regex("^[a-z0-9][a-z0-9_-]{0,62}$", var.environment))
    error_message = "environment must be a valid non-empty GCP label value."
  }
}

variable "owner" {
  description = "Accountable team label applied to node resources"
  type        = string

  validation {
    condition     = can(regex("^[a-z0-9][a-z0-9_-]{0,62}$", var.owner))
    error_message = "owner must be a valid non-empty GCP label value."
  }
}

variable "data_classification" {
  description = "Data-classification label applied to node resources"
  type        = string
  default     = "internal"

  validation {
    condition     = contains(["public", "internal", "confidential", "restricted"], var.data_classification)
    error_message = "data_classification must be public, internal, confidential, or restricted."
  }
}

variable "resource_labels" {
  description = "Additional GCP resource labels; module governance labels take precedence"
  type        = map(string)
  default     = {}

  validation {
    condition = length(var.resource_labels) <= 58 && alltrue([
      for key, value in var.resource_labels :
      can(regex("^[a-z][a-z0-9_-]{0,62}$", key)) &&
      can(regex("^[a-z0-9_-]{0,63}$", value))
    ])
    error_message = "resource_labels must contain at most 58 valid lowercase pairs, leaving room for module governance labels."
  }
}

variable "node_labels" {
  description = "Additional Kubernetes node labels; module identity labels take precedence"
  type        = map(string)
  default     = {}

  validation {
    condition = (
      alltrue([
        for key, value in var.node_labels :
        length(regexall("/", key)) <= 1 &&
        length(element(reverse(split("/", key)), 0)) >= 1 &&
        length(element(reverse(split("/", key)), 0)) <= 63 &&
        can(regex("^[A-Za-z0-9](?:[-A-Za-z0-9_.]{0,61}[A-Za-z0-9])?$", element(reverse(split("/", key)), 0))) &&
        (
          length(split("/", key)) == 1 || (
            length(split("/", key)[0]) >= 1 &&
            length(split("/", key)[0]) <= 253 &&
            alltrue([
              for segment in split(".", split("/", key)[0]) :
              length(segment) >= 1 &&
              length(segment) <= 63 &&
              can(regex("^[a-z0-9](?:[-a-z0-9]{0,61}[a-z0-9])?$", segment))
            ])
          )
        ) &&
        length(value) <= 63 &&
        can(regex("^$|^[A-Za-z0-9](?:[-A-Za-z0-9_.]{0,61}[A-Za-z0-9])?$", value))
      ]) &&
      sum(concat([0], [
        for key, value in merge(var.node_labels, {
          "mindclade.dev/gpu-profile" = var.profile
          "mindclade.dev/node-pool"   = "gpu"
        }) : length(key) + length(value)
      ])) < 1024
    )
    error_message = "node_labels keys must have an optional DNS-subdomain prefix up to 253 characters and a 1-63 character name; values must be empty or valid 1-63 character Kubernetes label values; the final merged map must total less than 1,024 characters."
  }
}

variable "additional_taints" {
  description = "Additional Kubernetes taints; nvidia.com/gpu is managed by this module"
  type = list(object({
    key    = string
    value  = string
    effect = string
  }))
  default = []

  validation {
    condition = alltrue([
      for taint in var.additional_taints :
      length(regexall("/", taint.key)) <= 1 &&
      length(element(reverse(split("/", taint.key)), 0)) >= 1 &&
      length(element(reverse(split("/", taint.key)), 0)) <= 63 &&
      can(regex("^[A-Za-z0-9](?:[-A-Za-z0-9_.]{0,61}[A-Za-z0-9])?$", element(reverse(split("/", taint.key)), 0))) &&
      (
        length(split("/", taint.key)) == 1 || (
          length(split("/", taint.key)[0]) >= 1 &&
          length(split("/", taint.key)[0]) <= 253 &&
          alltrue([
            for segment in split(".", split("/", taint.key)[0]) :
            length(segment) >= 1 &&
            length(segment) <= 63 &&
            can(regex("^[a-z0-9](?:[-a-z0-9]{0,61}[a-z0-9])?$", segment))
          ])
        )
      ) &&
      length(taint.value) <= 63 &&
      can(regex("^$|^[A-Za-z0-9](?:[-A-Za-z0-9_.]{0,61}[A-Za-z0-9])?$", taint.value)) &&
      contains(["NO_SCHEDULE", "PREFER_NO_SCHEDULE", "NO_EXECUTE"], taint.effect)
    ])
    error_message = "Each additional taint needs a Kubernetes-qualified key, an empty or valid 1-63 character label value, and an effect of NO_SCHEDULE, PREFER_NO_SCHEDULE, or NO_EXECUTE."
  }
}

variable "capacity_mode" {
  description = "Capacity acquisition mode; quota, reservation, and Dynamic Workload Scheduler policy remain environment responsibilities"
  type        = string
  default     = "ON_DEMAND"

  validation {
    condition = contains([
      "FLEX_START",
      "ON_DEMAND",
      "QUEUED_PROVISIONING",
      "RESERVATION",
      "SPOT",
    ], var.capacity_mode)
    error_message = "capacity_mode must be FLEX_START, ON_DEMAND, QUEUED_PROVISIONING, RESERVATION, or SPOT."
  }
}

variable "reservation_name" {
  description = "Specific same-region Compute Engine reservation consumed in RESERVATION mode"
  type        = string
  default     = null

  validation {
    condition     = var.reservation_name == null || can(regex("^[a-z][a-z0-9-]{0,61}[a-z0-9]$", var.reservation_name))
    error_message = "reservation_name must be null or a valid Compute Engine reservation name."
  }
}

variable "enable_preview_flex_start" {
  description = "Explicitly approve the standalone FLEX_START preview after environment qualification; ignored for the GA queued-provisioning combination"
  type        = bool
  default     = false
}

variable "max_run_duration" {
  description = "Maximum Flex Start or queued-provisioning node lifetime, bounded to seven days"
  type        = string
  default     = "86400s"

  validation {
    condition = (
      can(regex("^[1-9][0-9]*s$", var.max_run_duration)) &&
      try(tonumber(trimsuffix(var.max_run_duration, "s")), 0) <= 604800
    )
    error_message = "max_run_duration must be 1-604800 whole seconds, for example 86400s."
  }
}

variable "total_min_nodes" {
  description = "Minimum total nodes in this single-zone pool"
  type        = number
  default     = 0

  validation {
    condition     = var.total_min_nodes >= 0 && floor(var.total_min_nodes) == var.total_min_nodes
    error_message = "total_min_nodes must be a non-negative whole number."
  }
}

variable "total_max_nodes" {
  description = "Maximum total nodes; defaults to one two-node NOVA training slice"
  type        = number
  default     = 2

  validation {
    condition     = var.total_max_nodes >= 2 && floor(var.total_max_nodes) == var.total_max_nodes
    error_message = "total_max_nodes must be a whole number of at least 2."
  }
}

variable "max_pods_per_node" {
  description = "Maximum Pods per accelerator node"
  type        = number
  default     = 16

  validation {
    condition     = var.max_pods_per_node >= 8 && var.max_pods_per_node <= 64 && floor(var.max_pods_per_node) == var.max_pods_per_node
    error_message = "max_pods_per_node must be a whole number from 8 through 64."
  }
}

variable "boot_disk_size_gb" {
  description = "Boot-disk size for each accelerator node"
  type        = number
  default     = 250

  validation {
    condition     = var.boot_disk_size_gb >= 100 && floor(var.boot_disk_size_gb) == var.boot_disk_size_gb
    error_message = "boot_disk_size_gb must be a whole number of at least 100."
  }
}

variable "boot_disk_kms_key" {
  description = "Optional regional Cloud KMS CryptoKey for H100 pd-ssd boot disks; unsupported for the H200 Hyperdisk profile"
  type        = string
  default     = null

  validation {
    condition = var.boot_disk_kms_key == null || can(regex(
      "^projects/[a-z][a-z0-9-]{4,28}[a-z0-9]/locations/${var.region}/keyRings/[A-Za-z0-9_-]+/cryptoKeys/[A-Za-z0-9_-]+$",
      coalesce(var.boot_disk_kms_key, ""),
    ))
    error_message = "boot_disk_kms_key must be null or a complete Cloud KMS CryptoKey resource name in the node-pool region."
  }
}

variable "gpu_driver_version" {
  description = "GKE-managed NVIDIA driver channel"
  type        = string
  default     = "DEFAULT"

  validation {
    condition     = contains(["DEFAULT", "LATEST"], var.gpu_driver_version)
    error_message = "gpu_driver_version must be DEFAULT or LATEST; unmanaged driver installation is not supported by this module."
  }
}

variable "enable_compact_placement" {
  description = "Use a compact placement policy for lower-latency multi-node collectives"
  type        = bool
  default     = true
}

variable "upgrade_max_surge" {
  description = "Extra accelerator nodes allowed during a surge upgrade; requires quota/capacity"
  type        = number
  default     = 0

  validation {
    condition     = var.upgrade_max_surge >= 0 && var.upgrade_max_surge <= 20 && floor(var.upgrade_max_surge) == var.upgrade_max_surge
    error_message = "upgrade_max_surge must be a whole number from 0 through 20."
  }
}

variable "upgrade_max_unavailable" {
  description = "Accelerator nodes allowed to be unavailable during an upgrade"
  type        = number
  default     = 1

  validation {
    condition     = var.upgrade_max_unavailable >= 0 && var.upgrade_max_unavailable <= 20 && floor(var.upgrade_max_unavailable) == var.upgrade_max_unavailable
    error_message = "upgrade_max_unavailable must be a whole number from 0 through 20."
  }
}

variable "node_drain_grace_period" {
  description = "Grace period used when draining an accelerator node"
  type        = string
  default     = "3600s"

  validation {
    condition     = can(regex("^[1-9][0-9]*s$", var.node_drain_grace_period))
    error_message = "node_drain_grace_period must be a positive whole number of seconds."
  }
}

variable "node_drain_pdb_timeout" {
  description = "Maximum time to honor PodDisruptionBudgets during an accelerator-node drain"
  type        = string
  default     = "3600s"

  validation {
    condition     = can(regex("^[1-9][0-9]*s$", var.node_drain_pdb_timeout))
    error_message = "node_drain_pdb_timeout must be a positive whole number of seconds."
  }
}
