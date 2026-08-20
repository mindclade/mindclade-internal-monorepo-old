# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

variable "project_id" {
  description = "GCP project ID that owns the existing GKE cluster and dedicated node service account"
  type        = string

  validation {
    condition     = can(regex("^[a-z][a-z0-9-]{4,28}[a-z0-9]$", var.project_id))
    error_message = "project_id must be a valid 6-30 character GCP project ID."
  }
}

variable "cluster_name" {
  description = "Existing regional GKE Standard cluster name"
  type        = string

  validation {
    condition     = can(regex("^[a-z][a-z0-9-]{0,38}[a-z0-9]$", var.cluster_name))
    error_message = "cluster_name must be a valid 2-40 character GKE cluster name."
  }
}

variable "name" {
  description = "GKE node-pool name"
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

variable "node_locations" {
  description = "One or more zones in region used by this pool"
  type        = set(string)

  validation {
    condition = length(var.node_locations) >= 1 && length(var.node_locations) <= 3 && alltrue([
      for zone in var.node_locations : can(regex("^[a-z]+(?:-[a-z0-9]+)+[0-9]-[a-z]$", zone))
    ])
    error_message = "node_locations must contain 1-3 valid GCP zones."
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

variable "service_account_id" {
  description = "Account ID for the dedicated user-managed node VM service account created by this module"
  type        = string

  validation {
    condition     = can(regex("^[a-z][a-z0-9-]{4,28}[a-z0-9]$", var.service_account_id))
    error_message = "service_account_id must be a valid 6-30 character service-account ID."
  }
}

variable "service_account_display_name" {
  description = "Optional human-readable display name for the dedicated node service account"
  type        = string
  default     = null

  validation {
    condition     = var.service_account_display_name == null || (length(trimspace(var.service_account_display_name)) >= 3 && length(var.service_account_display_name) <= 100)
    error_message = "service_account_display_name must be null or contain 3-100 characters."
  }
}

variable "profile" {
  description = "Reviewed workload profile controlling the default machine type and isolation taints"
  type        = string
  default     = "GENERAL_PURPOSE"

  validation {
    condition     = contains(["GENERAL_PURPOSE", "HIGH_MEMORY"], var.profile)
    error_message = "profile must be GENERAL_PURPOSE or HIGH_MEMORY."
  }
}

variable "machine_type" {
  description = "Optional Compute Engine machine-type override; profile defaults are n2-standard-8 and n2-highmem-8"
  type        = string
  default     = null

  validation {
    condition     = var.machine_type == null || can(regex("^[a-z][a-z0-9-]{1,62}$", coalesce(var.machine_type, "")))
    error_message = "machine_type must be null or a valid machine-type name."
  }
}

variable "capacity_type" {
  description = "Node capacity type; Spot is interruptible and requires an explicit acknowledgement"
  type        = string
  default     = "ON_DEMAND"

  validation {
    condition     = contains(["ON_DEMAND", "SPOT"], var.capacity_type)
    error_message = "capacity_type must be ON_DEMAND or SPOT."
  }
}

variable "spot_approval" {
  description = "Exact acknowledgement required for interruptible Spot nodes"
  type        = string
  default     = null
}

variable "environment" {
  description = "Environment governance label"
  type        = string

  validation {
    condition     = can(regex("^[a-z0-9][a-z0-9_-]{0,62}$", var.environment))
    error_message = "environment must be a valid non-empty GCP label value."
  }
}

variable "owner" {
  description = "Accountable team governance label"
  type        = string

  validation {
    condition     = can(regex("^[a-z0-9][a-z0-9_-]{0,62}$", var.owner))
    error_message = "owner must be a valid non-empty GCP label value."
  }
}

variable "data_classification" {
  description = "Data-classification governance label"
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
          "mindclade.dev/capacity-type"    = lower(replace(var.capacity_type, "_", "-"))
          "mindclade.dev/node-pool"        = "cpu"
          "mindclade.dev/workload-profile" = lower(replace(var.profile, "_", "-"))
        }) : length(key) + length(value)
      ])) < 1024
    )
    error_message = "node_labels must use Kubernetes-qualified keys and values, and the final merged map must total less than 1,024 characters."
  }
}

variable "additional_taints" {
  description = "Additional Kubernetes taints; high-memory and Spot isolation keys are module-managed"
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
    error_message = "Each additional taint needs a Kubernetes-qualified key, a valid value, and a supported effect."
  }
}

variable "total_min_nodes" {
  description = "Minimum total nodes across all node_locations"
  type        = number
  default     = 1

  validation {
    condition     = var.total_min_nodes >= 0 && floor(var.total_min_nodes) == var.total_min_nodes
    error_message = "total_min_nodes must be a non-negative whole number."
  }
}

variable "total_max_nodes" {
  description = "Maximum total nodes across all node_locations"
  type        = number
  default     = 10

  validation {
    condition     = var.total_max_nodes >= 1 && floor(var.total_max_nodes) == var.total_max_nodes
    error_message = "total_max_nodes must be a positive whole number."
  }
}

variable "max_pods_per_node" {
  description = "Maximum Pods scheduled per node"
  type        = number
  default     = 64

  validation {
    condition     = var.max_pods_per_node >= 8 && var.max_pods_per_node <= 110 && floor(var.max_pods_per_node) == var.max_pods_per_node
    error_message = "max_pods_per_node must be a whole number from 8 through 110."
  }
}

variable "boot_disk_type" {
  description = "Boot-disk type for node VMs"
  type        = string
  default     = "pd-balanced"

  validation {
    condition     = contains(["pd-balanced", "pd-ssd"], var.boot_disk_type)
    error_message = "boot_disk_type must be pd-balanced or pd-ssd."
  }
}

variable "boot_disk_size_gb" {
  description = "Boot-disk size for each node"
  type        = number
  default     = 100

  validation {
    condition     = var.boot_disk_size_gb >= 50 && floor(var.boot_disk_size_gb) == var.boot_disk_size_gb
    error_message = "boot_disk_size_gb must be a whole number of at least 50."
  }
}

variable "boot_disk_kms_key" {
  description = "Optional regional Cloud KMS CryptoKey used for node boot disks"
  type        = string
  default     = null

  validation {
    condition = var.boot_disk_kms_key == null || can(regex(
      "^projects/[a-z][a-z0-9-]{4,28}[a-z0-9]/locations/${var.region}/keyRings/[A-Za-z0-9_-]+/cryptoKeys/[A-Za-z0-9_-]+$",
      coalesce(var.boot_disk_kms_key, ""),
    ))
    error_message = "boot_disk_kms_key must be null or a complete CryptoKey resource name in region."
  }
}

variable "pod_pids_limit" {
  description = "Per-Pod process ID limit enforced by kubelet"
  type        = number
  default     = 4096

  validation {
    condition     = var.pod_pids_limit >= 1024 && var.pod_pids_limit <= 4194304 && floor(var.pod_pids_limit) == var.pod_pids_limit
    error_message = "pod_pids_limit must be a whole number from 1024 through 4194304."
  }
}

variable "upgrade_max_surge" {
  description = "Additional nodes permitted during a surge upgrade"
  type        = number
  default     = 1

  validation {
    condition     = var.upgrade_max_surge >= 0 && floor(var.upgrade_max_surge) == var.upgrade_max_surge
    error_message = "upgrade_max_surge must be a non-negative whole number."
  }
}

variable "upgrade_max_unavailable" {
  description = "Nodes allowed to be unavailable during an upgrade"
  type        = number
  default     = 0

  validation {
    condition     = var.upgrade_max_unavailable >= 0 && floor(var.upgrade_max_unavailable) == var.upgrade_max_unavailable
    error_message = "upgrade_max_unavailable must be a non-negative whole number."
  }
}

variable "node_drain_grace_period" {
  description = "Maximum node drain grace period"
  type        = string
  default     = "900s"

  validation {
    condition     = can(regex("^[1-9][0-9]*s$", var.node_drain_grace_period))
    error_message = "node_drain_grace_period must be a positive whole-second duration such as 900s."
  }
}

variable "node_drain_pdb_timeout" {
  description = "Maximum time GKE waits for Pod disruption budgets during drain"
  type        = string
  default     = "600s"

  validation {
    condition     = can(regex("^[1-9][0-9]*s$", var.node_drain_pdb_timeout))
    error_message = "node_drain_pdb_timeout must be a positive whole-second duration such as 600s."
  }
}
