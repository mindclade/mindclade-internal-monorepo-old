# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

variable "project_id" {
  description = "Project containing the GKE cluster, worker pool, and executor identity"
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
    condition     = can(regex("^[a-z](?:[-a-z0-9]{0,38}[a-z0-9])?$", var.cluster_name))
    error_message = "cluster_name must be a valid lowercase GKE cluster name."
  }
}

variable "node_pool_name" {
  description = "Dedicated Bazel remote execution node-pool name"
  type        = string
  default     = "bazel-executors"

  validation {
    condition     = can(regex("^[a-z](?:[-a-z0-9]{0,38}[a-z0-9])?$", var.node_pool_name))
    error_message = "node_pool_name must be a valid lowercase GKE node-pool name."
  }
}

variable "region" {
  description = "Region of the existing cluster"
  type        = string

  validation {
    condition     = can(regex("^[a-z]+(?:-[a-z0-9]+)+[0-9]$", var.region))
    error_message = "region must be a regional location such as us-central1."
  }
}

variable "node_locations" {
  description = "Two or more zones in region used by the worker pool"
  type        = set(string)

  validation {
    condition = length(var.node_locations) >= 2 && alltrue([
      for zone in var.node_locations : startswith(zone, "${var.region}-")
    ])
    error_message = "node_locations must contain at least two zones in region."
  }
}

variable "pod_secondary_range_name" {
  description = "Existing pod secondary range used by the cluster"
  type        = string

  validation {
    condition     = can(regex("^[a-z][a-z0-9-]{0,62}$", var.pod_secondary_range_name))
    error_message = "pod_secondary_range_name must be a valid secondary range name."
  }
}

variable "node_service_account_id" {
  description = "Account ID for the dedicated node VM service account created by cpu_node_pool"
  type        = string

  validation {
    condition     = can(regex("^[a-z](?:[-a-z0-9]{4,28}[a-z0-9])$", var.node_service_account_id))
    error_message = "node_service_account_id must be a valid 6-30 character service-account ID."
  }
}

variable "executor_service_account_id" {
  description = "Account ID for the keyless Bazel executor workload service account"
  type        = string
  default     = "bazel-remote-executor"

  validation {
    condition     = can(regex("^[a-z](?:[-a-z0-9]{4,28}[a-z0-9])$", var.executor_service_account_id))
    error_message = "executor_service_account_id must be a valid 6-30 character service-account ID."
  }
}

variable "kubernetes_namespace" {
  description = "Namespace of the executor Kubernetes service account deployed by GitOps"
  type        = string
  default     = "build"

  validation {
    condition     = can(regex("^[a-z0-9](?:[-a-z0-9]{0,61}[a-z0-9])?$", var.kubernetes_namespace))
    error_message = "kubernetes_namespace must be an RFC1123 label."
  }
}

variable "kubernetes_service_account" {
  description = "Executor Kubernetes service account deployed by GitOps"
  type        = string
  default     = "bazel-remote-executor"

  validation {
    condition     = can(regex("^[a-z0-9](?:[-a-z0-9]{0,61}[a-z0-9])?$", var.kubernetes_service_account))
    error_message = "kubernetes_service_account must be an RFC1123 label."
  }
}

variable "executor_image" {
  description = "Immutable executor image reference deployed by GitOps; tags are forbidden"
  type        = string

  validation {
    condition     = can(regex("^[^[:space:]@]+@sha256:[0-9a-f]{64}$", var.executor_image))
    error_message = "executor_image must be an immutable registry reference ending in @sha256:<64 lowercase hex characters>."
  }
}

variable "cache_bucket_name" {
  description = "Existing bucket created by bazel_remote_cache"
  type        = string

  validation {
    condition     = length(var.cache_bucket_name) >= 3 && length(var.cache_bucket_name) <= 63 && can(regex("^[a-z0-9][a-z0-9._-]*[a-z0-9]$", var.cache_bucket_name))
    error_message = "cache_bucket_name must be a valid 3-63 character Cloud Storage bucket name."
  }
}

variable "executor_project_roles" {
  description = "Additional additive project roles for the executor workload; administrative and basic roles are forbidden"
  type        = set(string)
  default     = ["roles/artifactregistry.reader"]

  validation {
    condition = alltrue([
      for role in var.executor_project_roles :
      can(regex("^roles/[A-Za-z0-9_.]+$", role)) &&
      !contains(["roles/owner", "roles/editor", "roles/viewer"], role) &&
      !endswith(lower(role), ".admin")
    ])
    error_message = "executor_project_roles must contain predefined non-basic, non-admin roles."
  }
}

variable "environment" {
  description = "Environment governance label"
  type        = string

  validation {
    condition     = can(regex("^[a-z0-9][a-z0-9_-]{0,62}$", var.environment))
    error_message = "environment must be a valid GCP label value."
  }
}

variable "owner" {
  description = "Accountable team governance label"
  type        = string

  validation {
    condition     = can(regex("^[a-z0-9][a-z0-9_-]{0,62}$", var.owner))
    error_message = "owner must be a valid GCP label value."
  }
}

variable "profile" {
  description = "GENERAL_PURPOSE or HIGH_MEMORY CPU worker profile"
  type        = string
  default     = "GENERAL_PURPOSE"

  validation {
    condition     = contains(["GENERAL_PURPOSE", "HIGH_MEMORY"], var.profile)
    error_message = "profile must be GENERAL_PURPOSE or HIGH_MEMORY."
  }
}

variable "machine_type" {
  description = "Optional reviewed machine-type override"
  type        = string
  default     = null
}

variable "capacity_type" {
  description = "ON_DEMAND or explicitly acknowledged SPOT worker capacity"
  type        = string
  default     = "ON_DEMAND"

  validation {
    condition     = contains(["ON_DEMAND", "SPOT"], var.capacity_type)
    error_message = "capacity_type must be ON_DEMAND or SPOT."
  }
}

variable "spot_approval" {
  description = "Exact acknowledgement required for interruption-prone remote workers: I ACCEPT EVICTION AND CAPACITY-LOSS RISK"
  type        = string
  default     = null
  sensitive   = true
}

variable "total_min_nodes" {
  description = "Minimum worker nodes across all node locations"
  type        = number
  default     = 1

  validation {
    condition     = var.total_min_nodes >= 0 && floor(var.total_min_nodes) == var.total_min_nodes
    error_message = "total_min_nodes must be a non-negative whole number."
  }
}

variable "total_max_nodes" {
  description = "Maximum worker nodes across all node locations"
  type        = number
  default     = 20

  validation {
    condition     = var.total_max_nodes >= 1 && var.total_max_nodes <= 100 && floor(var.total_max_nodes) == var.total_max_nodes
    error_message = "total_max_nodes must be a whole number from 1 through 100."
  }
}

variable "max_pods_per_node" {
  description = "Maximum pods per worker node"
  type        = number
  default     = 32
}

variable "boot_disk_type" {
  description = "Worker boot disk type"
  type        = string
  default     = "pd-balanced"
}

variable "boot_disk_size_gb" {
  description = "Worker boot disk size in GiB"
  type        = number
  default     = 200
}

variable "boot_disk_kms_key" {
  description = "Optional regional CMEK CryptoKey for worker boot disks"
  type        = string
  default     = null
}

variable "resource_labels" {
  description = "Additional GCP labels; module governance labels take precedence"
  type        = map(string)
  default     = {}
}

variable "node_labels" {
  description = "Additional Kubernetes node labels; the workload label is module-owned"
  type        = map(string)
  default     = {}

  validation {
    condition     = !contains(keys(var.node_labels), "mindclade.dev/workload")
    error_message = "mindclade.dev/workload is reserved by the module."
  }
}

variable "additional_taints" {
  description = "Additional dedicated-pool taints"
  type = list(object({
    key    = string
    value  = string
    effect = string
  }))
  default = []

  validation {
    condition = alltrue([
      for taint in var.additional_taints :
      taint.key != "mindclade.dev/workload" &&
      contains(["NO_SCHEDULE", "PREFER_NO_SCHEDULE", "NO_EXECUTE"], taint.effect)
    ])
    error_message = "Additional taints cannot override the workload taint and must use a supported effect."
  }
}

variable "upgrade_max_surge" {
  description = "Maximum surge nodes during upgrades"
  type        = number
  default     = 1
}

variable "upgrade_max_unavailable" {
  description = "Maximum unavailable nodes during upgrades"
  type        = number
  default     = 0
}

variable "node_drain_grace_period" {
  description = "Grace period for node-pool deletion drains"
  type        = string
  default     = "600s"
}

variable "node_drain_pdb_timeout" {
  description = "PodDisruptionBudget timeout for node-pool deletion drains"
  type        = string
  default     = "600s"
}
