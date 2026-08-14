variable "project_id" {
  description = "GCP project ID that owns the cluster"
  type        = string

  validation {
    condition     = can(regex("^[a-z][a-z0-9-]{4,28}[a-z0-9]$", var.project_id))
    error_message = "project_id must be a valid 6-30 character GCP project ID."
  }
}

variable "name" {
  description = "Regional GKE cluster name; limited to leave room for the managed system node-pool suffix"
  type        = string

  validation {
    condition     = can(regex("^[a-z][a-z0-9-]{0,30}[a-z0-9]$", var.name))
    error_message = "name must be 2-32 lowercase letters, digits, or hyphens, beginning with a letter and ending with a letter or digit."
  }
}

variable "region" {
  description = "GCP region for the regional control plane, for example us-central1"
  type        = string

  validation {
    condition     = can(regex("^[a-z]+(?:-[a-z0-9]+)+[0-9]$", var.region))
    error_message = "region must be a regional location such as us-central1, not a zone."
  }
}

variable "network" {
  description = "Existing VPC network name or self-link"
  type        = string

  validation {
    condition     = length(trimspace(var.network)) > 0 && !can(regex("\\s", var.network))
    error_message = "network must be a non-empty network name or self-link without whitespace."
  }
}

variable "subnetwork" {
  description = "Existing regional subnetwork name or self-link"
  type        = string

  validation {
    condition     = length(trimspace(var.subnetwork)) > 0 && !can(regex("\\s", var.subnetwork))
    error_message = "subnetwork must be a non-empty subnetwork name or self-link without whitespace."
  }
}

variable "pod_secondary_range_name" {
  description = "Existing subnetwork secondary range used for Pod addresses"
  type        = string

  validation {
    condition     = can(regex("^[a-z][a-z0-9-]{0,62}$", var.pod_secondary_range_name))
    error_message = "pod_secondary_range_name must be a valid secondary range name."
  }
}

variable "service_secondary_range_name" {
  description = "Existing subnetwork secondary range used for Service addresses"
  type        = string

  validation {
    condition     = can(regex("^[a-z][a-z0-9-]{0,62}$", var.service_secondary_range_name))
    error_message = "service_secondary_range_name must be a valid secondary range name."
  }
}

variable "master_ipv4_cidr_block" {
  description = "Dedicated non-overlapping RFC1918 /28 for the private GKE control plane"
  type        = string

  validation {
    condition = (
      can(cidrnetmask(var.master_ipv4_cidr_block)) &&
      can(regex("/28$", var.master_ipv4_cidr_block)) &&
      (
        can(regex("^10\\.", var.master_ipv4_cidr_block)) ||
        can(regex("^172\\.(?:1[6-9]|2[0-9]|3[01])\\.", var.master_ipv4_cidr_block)) ||
        can(regex("^192\\.168\\.", var.master_ipv4_cidr_block))
      )
    )
    error_message = "master_ipv4_cidr_block must be a valid RFC1918 IPv4 /28 CIDR."
  }
}

variable "master_authorized_networks" {
  description = "Non-public CIDRs permitted to reach the private control-plane endpoint"
  type = list(object({
    cidr_block   = string
    display_name = string
  }))

  validation {
    condition = (
      length(var.master_authorized_networks) > 0 &&
      alltrue([
        for network in var.master_authorized_networks :
        can(cidrnetmask(network.cidr_block)) &&
        network.cidr_block != "0.0.0.0/0" &&
        length(trimspace(network.display_name)) > 0 &&
        length(network.display_name) <= 63
      ])
    )
    error_message = "Provide at least one valid IPv4 management CIDR with a 1-63 character display name; 0.0.0.0/0 is forbidden."
  }
}

variable "system_node_service_account_email" {
  description = "Dedicated user-managed service account for system node VMs; grant its permissions outside this module"
  type        = string

  validation {
    condition     = can(regex("^[a-z][a-z0-9-]{4,28}[a-z0-9]@[a-z][a-z0-9-]{4,28}[a-z0-9]\\.iam\\.gserviceaccount\\.com$", var.system_node_service_account_email))
    error_message = "system_node_service_account_email must be a user-managed GCP service-account email."
  }
}

variable "rbac_security_group" {
  description = "Google Group configured as the GKE security-groups parent for group-based Kubernetes RBAC"
  type        = string

  validation {
    condition     = can(regex("^gke-security-groups@[A-Za-z0-9.-]+\\.[A-Za-z]{2,}$", var.rbac_security_group))
    error_message = "rbac_security_group must use the GKE-required gke-security-groups@DOMAIN form."
  }
}

variable "environment" {
  description = "Environment label applied to cluster and node resources"
  type        = string

  validation {
    condition     = can(regex("^[a-z0-9][a-z0-9_-]{0,62}$", var.environment))
    error_message = "environment must be a valid non-empty GCP label value."
  }
}

variable "owner" {
  description = "Accountable team label applied to cluster and node resources"
  type        = string

  validation {
    condition     = can(regex("^[a-z0-9][a-z0-9_-]{0,62}$", var.owner))
    error_message = "owner must be a valid non-empty GCP label value."
  }
}

variable "data_classification" {
  description = "Data-classification label applied to cluster and node resources"
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
    condition = length(var.resource_labels) <= 59 && alltrue([
      for key, value in var.resource_labels :
      can(regex("^[a-z][a-z0-9_-]{0,62}$", key)) &&
      can(regex("^[a-z0-9_-]{0,63}$", value))
    ])
    error_message = "resource_labels must contain at most 59 valid lowercase pairs, leaving room for module governance labels."
  }
}

variable "release_channel" {
  description = "Pinned GKE release channel for the NOVA v1 training substrate"
  type        = string
  default     = "REGULAR"

  validation {
    condition     = var.release_channel == "REGULAR"
    error_message = "release_channel must remain REGULAR for the immutable NOVA v1 training platform tuple."
  }
}

variable "kubernetes_version" {
  description = "Pinned minimum control-plane and initial system node version for the NOVA v1 training qualification tuple"
  type        = string
  default     = "1.35.6-gke.1127000"

  validation {
    condition     = var.kubernetes_version == "1.35.6-gke.1127000"
    error_message = "kubernetes_version must match the immutable NOVA v1 training platform lock."
  }
}

variable "maintenance_window" {
  description = "Recurring UTC maintenance window in RFC3339/RFC5545 form"
  type = object({
    start_time = string
    end_time   = string
    recurrence = string
  })
  default = {
    start_time = "2025-01-05T02:00:00Z"
    end_time   = "2025-01-05T10:00:00Z"
    recurrence = "FREQ=WEEKLY;BYDAY=SU"
  }

  validation {
    condition = (
      can(regex("^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z$", var.maintenance_window.start_time)) &&
      can(regex("^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z$", var.maintenance_window.end_time)) &&
      can(regex("^FREQ=(DAILY|WEEKLY)(;[A-Z]+=[A-Z0-9,]+)*$", var.maintenance_window.recurrence))
    )
    error_message = "maintenance_window requires UTC RFC3339 start/end timestamps and a DAILY or WEEKLY RFC5545 recurrence."
  }
}

variable "enable_gcs_fuse_csi_driver" {
  description = "Enable the managed Cloud Storage FUSE CSI driver; NOVA training requires this to remain false and uses generation-bound object APIs"
  type        = bool
  default     = false
}

variable "enable_backup_agent" {
  description = "Enable the Backup for GKE agent; backup plans, IAM, retention, and restore tests remain separate resources"
  type        = bool
  default     = true
}

variable "database_encryption_key_name" {
  description = "Optional regional CryptoKey for application-layer Kubernetes Secrets encryption; required for restricted data"
  type        = string
  default     = null

  validation {
    condition = var.database_encryption_key_name == null || can(regex(
      "^projects/[a-z][a-z0-9-]{4,28}[a-z0-9]/locations/${var.region}/keyRings/[A-Za-z0-9_-]+/cryptoKeys/[A-Za-z0-9_-]+$",
      coalesce(var.database_encryption_key_name, ""),
    ))
    error_message = "database_encryption_key_name must be null or a complete Cloud KMS CryptoKey resource name in the cluster region."
  }
}

variable "system_node_pool_machine_type" {
  description = "Machine type for the non-accelerator system node pool"
  type        = string
  default     = "e2-standard-4"

  validation {
    condition     = can(regex("^[a-z][a-z0-9-]{1,62}$", var.system_node_pool_machine_type))
    error_message = "system_node_pool_machine_type must be a valid Compute Engine machine-type name."
  }
}

variable "system_node_pool_total_min_nodes" {
  description = "Minimum total system nodes across the regional node pool"
  type        = number
  default     = 3

  validation {
    condition     = var.system_node_pool_total_min_nodes >= 3 && floor(var.system_node_pool_total_min_nodes) == var.system_node_pool_total_min_nodes
    error_message = "system_node_pool_total_min_nodes must be a whole number of at least 3."
  }
}

variable "system_node_pool_total_max_nodes" {
  description = "Maximum total system nodes across the regional node pool"
  type        = number
  default     = 9

  validation {
    condition     = var.system_node_pool_total_max_nodes >= 3 && floor(var.system_node_pool_total_max_nodes) == var.system_node_pool_total_max_nodes
    error_message = "system_node_pool_total_max_nodes must be a whole number of at least 3."
  }
}

variable "system_node_pool_max_pods_per_node" {
  description = "Maximum Pods per system node"
  type        = number
  default     = 64

  validation {
    condition     = var.system_node_pool_max_pods_per_node >= 8 && var.system_node_pool_max_pods_per_node <= 110 && floor(var.system_node_pool_max_pods_per_node) == var.system_node_pool_max_pods_per_node
    error_message = "system_node_pool_max_pods_per_node must be a whole number from 8 through 110."
  }
}

variable "system_node_pool_boot_disk_size_gb" {
  description = "Boot-disk size for system nodes"
  type        = number
  default     = 100

  validation {
    condition     = var.system_node_pool_boot_disk_size_gb >= 50 && floor(var.system_node_pool_boot_disk_size_gb) == var.system_node_pool_boot_disk_size_gb
    error_message = "system_node_pool_boot_disk_size_gb must be a whole number of at least 50."
  }
}
