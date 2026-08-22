# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

variable "project_id" { type = string }
variable "gke_backup" {
  type = object({
    plan_name           = string
    cluster             = string
    location            = string
    cron_schedule       = string
    all_namespaces      = bool
    excluded_namespaces = set(string)
    include_volume_data = bool
    include_secrets     = bool
    encryption_key      = string
    retention = object({
      backup_retain_days      = number
      backup_delete_lock_days = number
    })
  })
  validation {
    condition     = can(regex("^[0-9*]+ [0-9*]+ [0-9*]+ [0-9*]+ [0-9*]+$", var.gke_backup.cron_schedule)) && var.gke_backup.all_namespaces
    error_message = "The backup plan requires a five-field cron schedule and an all-namespace backup baseline."
  }
}
variable "bucket_replication" {
  type = map(object({
    source_bucket                              = string
    destination_bucket                         = string
    destination_region                         = string
    kms_key_name                               = string
    delete_objects_unique_in_sink              = bool
    delete_objects_from_source_after_transfer  = bool
    overwrite_objects_already_existing_in_sink = bool
    schedule                                   = string
    retention_days                             = number
  }))
  validation {
    condition = length(var.bucket_replication) > 0 && alltrue([
      for replica in values(var.bucket_replication) :
      !replica.delete_objects_unique_in_sink && !replica.delete_objects_from_source_after_transfer &&
      replica.retention_days >= 30 &&
      can(regex("^(?:0 \\*|[0-5]?[0-9] (?:[01]?[0-9]|2[0-3])) \\* \\* \\*$", replica.schedule)) &&
      can(regex(
        "^projects/[a-z][a-z0-9-]{4,28}[a-z0-9]/locations/${replica.destination_region}/keyRings/[A-Za-z0-9_-]+/cryptoKeys/[A-Za-z0-9_-]+$",
        replica.kms_key_name,
      ))
    ])
    error_message = "DR replicas must never propagate deletion, retain at least 30 days, use an hourly or daily UTC schedule, and use a CMEK in the destination region."
  }
}
variable "labels" {
  type    = map(string)
  default = {}
}
