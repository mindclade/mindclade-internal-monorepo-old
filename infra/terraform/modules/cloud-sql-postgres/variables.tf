variable "project_id" {
  description = "Project that owns the instance"
  type        = string
}

variable "name" {
  description = "Primary instance name"
  type        = string

  validation {
    condition     = can(regex("^[a-z][a-z0-9-]{0,96}[a-z0-9]$", var.name))
    error_message = "name must be a valid Cloud SQL instance name."
  }
}

variable "region" {
  description = "Primary instance region"
  type        = string

  validation {
    condition     = can(regex("^[a-z]+-[a-z0-9]+[0-9]$", var.region))
    error_message = "region must be a Google Cloud region such as us-central1."
  }
}

variable "database_version" {
  description = "Repository-qualified PostgreSQL version"
  type        = string
  default     = "POSTGRES_17"

  validation {
    condition     = var.database_version == "POSTGRES_17"
    error_message = "database_version must remain POSTGRES_17 until another major version completes migration and restore qualification."
  }
}

variable "edition" {
  description = "Repository-qualified Cloud SQL edition"
  type        = string
  default     = "ENTERPRISE"

  validation {
    condition     = var.edition == "ENTERPRISE"
    error_message = "edition must remain ENTERPRISE until an Enterprise Plus machine, disk, migration, and restore profile is qualified."
  }
}

variable "tier" {
  description = "Qualified Enterprise db-custom primary machine tier"
  type        = string
  default     = "db-custom-2-7680"

  validation {
    condition = contains([
      "db-custom-2-7680",
      "db-custom-4-15360",
      "db-custom-8-30720",
      "db-custom-16-61440",
      "db-custom-32-122880",
      "db-custom-64-245760",
      "db-custom-96-368640",
    ], var.tier)
    error_message = "tier must be one of the reviewed balanced Enterprise db-custom shapes."
  }
}

variable "disk_type" {
  description = "Primary and replica disk type"
  type        = string
  default     = "PD_SSD"

  validation {
    condition     = var.disk_type == "PD_SSD"
    error_message = "disk_type must remain PD_SSD for the qualified Enterprise db-custom profile."
  }
}

variable "disk_size_gb" {
  description = "Initial primary disk size in GiB"
  type        = number
  default     = 100

  validation {
    condition     = var.disk_size_gb >= 20 && var.disk_size_gb <= 65536 && floor(var.disk_size_gb) == var.disk_size_gb
    error_message = "disk_size_gb must be a whole number from 20 through the Cloud SQL maximum of 65536."
  }
}

variable "disk_autoresize_limit_gb" {
  description = "Storage growth cap; zero uses the service maximum and requires cost monitoring"
  type        = number
  default     = 1000

  validation {
    condition     = var.disk_autoresize_limit_gb >= 0 && var.disk_autoresize_limit_gb <= 65536 && floor(var.disk_autoresize_limit_gb) == var.disk_autoresize_limit_gb
    error_message = "disk_autoresize_limit_gb must be zero or a whole number no greater than 65536."
  }
}

variable "private_network" {
  description = "VPC resource name used for private IP"
  type        = string

  validation {
    condition     = can(regex("^projects/[^/]+/global/networks/[^/]+$", var.private_network))
    error_message = "private_network must use projects/PROJECT/global/networks/NETWORK."
  }
}

variable "allocated_ip_range" {
  description = "Optional private service access allocated range name"
  type        = string
  default     = null
}

variable "enable_private_google_access" {
  description = "Allow supported Google services to reach the instance through private paths"
  type        = bool
  default     = true
}

variable "connector_enforcement" {
  description = "Require Cloud SQL connectors instead of direct database connections"
  type        = string
  default     = "REQUIRED"

  validation {
    condition     = contains(["REQUIRED", "NOT_REQUIRED"], var.connector_enforcement)
    error_message = "connector_enforcement must be REQUIRED or NOT_REQUIRED."
  }
}

variable "backup_start_time_utc" {
  description = "Daily backup start in HH:MM UTC"
  type        = string
  default     = "05:00"

  validation {
    condition     = can(regex("^(?:[01][0-9]|2[0-3]):[0-5][0-9]$", var.backup_start_time_utc))
    error_message = "backup_start_time_utc must use HH:MM in UTC."
  }
}

variable "backup_location" {
  description = "Explicit backup region or multi-region chosen for the recovery design"
  type        = string
}

variable "transaction_log_retention_days" {
  description = "PITR transaction-log retention"
  type        = number
  default     = 7

  validation {
    condition     = var.transaction_log_retention_days == 7
    error_message = "transaction_log_retention_days must remain 7 for the qualified Enterprise edition profile."
  }
}

variable "retained_backups" {
  description = "Number of automated backups retained"
  type        = number
  default     = 14

  validation {
    condition = (
      var.retained_backups >= 14 && var.retained_backups <= 365 &&
      floor(var.retained_backups) == var.retained_backups &&
      var.retained_backups > var.transaction_log_retention_days
    )
    error_message = "retained_backups must be a whole number from 14 through 365 and exceed transaction-log retention days."
  }
}

variable "final_backup_retention_days" {
  description = "Retention for the final backup after an approved deletion"
  type        = number
  default     = 30

  validation {
    condition     = var.final_backup_retention_days >= 14 && var.final_backup_retention_days <= 365 && floor(var.final_backup_retention_days) == var.final_backup_retention_days
    error_message = "final_backup_retention_days must be a whole number from 14 through 365."
  }
}

variable "maintenance_day" {
  description = "Maintenance weekday numbered 1 (Monday) through 7 (Sunday)"
  type        = number
  default     = 7

  validation {
    condition     = var.maintenance_day >= 1 && var.maintenance_day <= 7 && floor(var.maintenance_day) == var.maintenance_day
    error_message = "maintenance_day must be 1 through 7."
  }
}

variable "maintenance_hour_utc" {
  description = "Maintenance start hour in UTC"
  type        = number
  default     = 7

  validation {
    condition     = var.maintenance_hour_utc >= 0 && var.maintenance_hour_utc <= 23 && floor(var.maintenance_hour_utc) == var.maintenance_hour_utc
    error_message = "maintenance_hour_utc must be 0 through 23."
  }
}

variable "maintenance_update_track" {
  description = "Service update rollout track"
  type        = string
  default     = "stable"

  validation {
    condition     = contains(["canary", "stable", "week5"], var.maintenance_update_track)
    error_message = "maintenance_update_track must be canary, stable, or week5."
  }
}

variable "query_insights_enabled" {
  description = "Enable Query Insights; data-governance owners must approve query text retention"
  type        = bool
  default     = true
}

variable "query_plans_per_minute" {
  description = "Query execution plans captured per minute"
  type        = number
  default     = 5

  validation {
    condition     = floor(var.query_plans_per_minute) == var.query_plans_per_minute && var.query_plans_per_minute >= 0 && var.query_plans_per_minute <= 20
    error_message = "query_plans_per_minute must be a whole number from 0 through 20 for Enterprise edition."
  }
}

variable "query_string_length" {
  description = "Maximum query text length retained by Query Insights"
  type        = number
  default     = 1024

  validation {
    condition     = floor(var.query_string_length) == var.query_string_length && var.query_string_length >= 256 && var.query_string_length <= 4500
    error_message = "query_string_length must be a whole number from 256 through 4500 for Enterprise edition."
  }
}

variable "database_flags" {
  description = "Additional reviewed PostgreSQL flags; mandatory audit flags cannot be overridden"
  type        = map(string)
  default = {
    "cloudsql.iam_authentication" = "on"
  }

  validation {
    condition = length(setintersection(toset(keys(var.database_flags)), toset([
      "cloudsql.enable_pgaudit",
      "log_checkpoints",
      "log_connections",
      "log_disconnections",
      "log_hostname",
      "log_lock_waits",
      "log_min_error_statement",
      "log_statement",
    ]))) == 0
    error_message = "database_flags cannot override the module's mandatory PostgreSQL audit flags."
  }
}

variable "databases" {
  description = "Databases to create without credentials"
  type = map(object({
    charset   = optional(string, "UTF8")
    collation = optional(string, "en_US.UTF8")
  }))
  default = {}
}

variable "iam_database_users" {
  description = "Passwordless Cloud IAM database principals; names must follow Cloud SQL PostgreSQL conventions"
  type = set(object({
    name = string
    type = string
  }))
  default = []

  validation {
    condition     = alltrue([for user in var.iam_database_users : contains(["CLOUD_IAM_USER", "CLOUD_IAM_SERVICE_ACCOUNT", "CLOUD_IAM_GROUP"], user.type)])
    error_message = "IAM user type must be CLOUD_IAM_USER, CLOUD_IAM_SERVICE_ACCOUNT, or CLOUD_IAM_GROUP."
  }

  validation {
    condition     = length(var.iam_database_users) == length(distinct([for user in var.iam_database_users : user.name]))
    error_message = "Each IAM database user name must be unique regardless of principal type."
  }
}

variable "read_replicas" {
  description = "Optional cross-region read replicas; promotion and failback remain runbook-controlled"
  type = map(object({
    region             = string
    tier               = string
    private_network    = string
    allocated_ip_range = optional(string)
    kms_key_name       = optional(string)
  }))
  default = {}

  validation {
    condition     = alltrue([for replica in values(var.read_replicas) : replica.region != var.region])
    error_message = "read_replicas must be in regions distinct from the primary."
  }

  validation {
    condition = alltrue([
      for name, replica in var.read_replicas :
      can(regex("^[a-z][a-z0-9-]{0,96}[a-z0-9]$", name)) &&
      can(regex("^[a-z]+-[a-z0-9]+[0-9]$", replica.region)) &&
      contains([
        "db-custom-2-7680",
        "db-custom-4-15360",
        "db-custom-8-30720",
        "db-custom-16-61440",
        "db-custom-32-122880",
        "db-custom-64-245760",
        "db-custom-96-368640",
      ], replica.tier) &&
      replica.private_network == var.private_network &&
      can(regex("^projects/[^/]+/global/networks/[^/]+$", replica.private_network))
    ])
    error_message = "Each read replica needs a valid name, region, Enterprise db-custom tier, and the same VPC resource name as the primary."
  }

  validation {
    condition = alltrue([
      for replica in values(var.read_replicas) :
      (var.kms_key_name == null) == (replica.kms_key_name == null) &&
      (
        replica.kms_key_name == null ||
        can(regex("^projects/[^/]+/locations/${replica.region}/keyRings/[^/]+/cryptoKeys/[^/]+$", coalesce(replica.kms_key_name, "")))
      )
    ])
    error_message = "Replicas must use Google-managed encryption when the primary does, or a full region-local CMEK when the primary uses CMEK."
  }
}

variable "kms_key_name" {
  description = "Optional regional CryptoKey for the primary; grant the Cloud SQL service agent separately"
  type        = string
  default     = null

  validation {
    condition = (
      var.kms_key_name == null ||
      can(regex("^projects/[^/]+/locations/${var.region}/keyRings/[^/]+/cryptoKeys/[^/]+$", coalesce(var.kms_key_name, "")))
    )
    error_message = "kms_key_name must be null or a full CryptoKey resource name in the primary region."
  }
}

variable "environment" {
  description = "Environment governance label"
  type        = string
}

variable "owner" {
  description = "Accountable team governance label"
  type        = string
}

variable "data_classification" {
  description = "Data-classification governance label"
  type        = string
  default     = "confidential"

  validation {
    condition     = contains(["internal", "confidential", "restricted"], var.data_classification)
    error_message = "Cloud SQL data must be internal, confidential, or restricted."
  }
}

variable "labels" {
  description = "Additional labels; baseline governance labels take precedence"
  type        = map(string)
  default     = {}

  validation {
    condition = length(var.labels) <= 59 && alltrue([
      for key, value in var.labels :
      can(regex("^[a-z][a-z0-9_-]{0,62}$", key)) &&
      can(regex("^$|^[a-z0-9][a-z0-9_-]{0,62}$", value))
    ])
    error_message = "labels must contain at most 59 valid lowercase pairs, leaving room for replica governance labels."
  }
}
