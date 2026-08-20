# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

variable "project_id" {
  description = "Project that owns the Pub/Sub resources."
  type        = string

  validation {
    condition     = can(regex("^[a-z][a-z0-9-]{4,28}[a-z0-9]$", var.project_id))
    error_message = "project_id must be a valid Google Cloud project ID."
  }
}

variable "environment" {
  description = "Environment governance label."
  type        = string

  validation {
    condition     = can(regex("^[a-z0-9][a-z0-9_-]{0,62}$", var.environment))
    error_message = "environment must be a valid Google Cloud label value."
  }
}

variable "owner" {
  description = "Accountable team governance label."
  type        = string

  validation {
    condition     = can(regex("^[a-z0-9][a-z0-9_-]{0,62}$", var.owner))
    error_message = "owner must be a valid Google Cloud label value."
  }
}

variable "labels" {
  description = "Additional labels; baseline environment, owner, and managed-by labels take precedence."
  type        = map(string)
  default     = {}

  validation {
    condition = length(var.labels) <= 61 && alltrue([
      for key, value in var.labels :
      can(regex("^[a-z][a-z0-9_-]{0,62}$", key)) &&
      can(regex("^$|^[a-z0-9][a-z0-9_-]{0,62}$", value))
    ])
    error_message = "labels must contain at most 61 valid lowercase pairs."
  }
}

variable "pubsub_service_agent_email" {
  description = "Google-managed Pub/Sub service-agent email for project_id, used for CMEK and dead-letter contracts."
  type        = string

  validation {
    condition     = can(regex("^service-[0-9]+@gcp-sa-pubsub[.]iam[.]gserviceaccount[.]com$", var.pubsub_service_agent_email))
    error_message = "pubsub_service_agent_email must be service-<project-number>@gcp-sa-pubsub.iam.gserviceaccount.com."
  }
}

variable "schemas" {
  description = "Schemas keyed by stable Terraform identity. Topics can pin the managed revision."
  type = map(object({
    name       = string
    type       = string
    definition = string
    viewers    = optional(set(string), [])
  }))
  default = {}

  validation {
    condition = alltrue([
      for schema in values(var.schemas) :
      can(regex("^[A-Za-z][A-Za-z0-9._~+%-]{2,254}$", schema.name)) &&
      !startswith(lower(schema.name), "goog") &&
      contains(["AVRO", "PROTOCOL_BUFFER"], schema.type) &&
      length(trimspace(schema.definition)) > 0 &&
      length(schema.definition) <= 300000
    ])
    error_message = "Schema names, AVRO/PROTOCOL_BUFFER type, and non-empty definitions of at most 300000 characters are required."
  }

  validation {
    condition = alltrue(flatten([
      for schema in values(var.schemas) : [
        for member in schema.viewers :
        !contains(["allUsers", "allAuthenticatedUsers"], member) &&
        can(regex("^(user|group|serviceAccount|domain):[^[:space:]]+$|^principal(Set)?://iam[.]googleapis[.]com/.+$", member))
      ]
    ]))
    error_message = "Schema viewers must be non-empty, non-public IAM members."
  }

  validation {
    condition     = length(distinct([for schema in values(var.schemas) : schema.name])) == length(var.schemas)
    error_message = "Schema resource names must be unique."
  }
}

variable "topics" {
  description = "CMEK-encrypted topics with bounded retention, residency, optional schema enforcement, and additive IAM."
  type = map(object({
    name                        = string
    kms_key_name                = string
    message_retention_seconds   = optional(number, 604800)
    allowed_persistence_regions = set(string)
    labels                      = optional(map(string), {})
    publishers                  = optional(set(string), [])
    viewers                     = optional(set(string), [])
    schema = optional(object({
      managed_schema_key   = optional(string)
      schema_resource_name = optional(string)
      encoding             = optional(string, "JSON")
      first_revision_id    = optional(string)
      last_revision_id     = optional(string)
    }))
  }))
  default = {}

  validation {
    condition = alltrue([
      for topic in values(var.topics) :
      can(regex("^[A-Za-z][A-Za-z0-9._~+%-]{2,254}$", topic.name)) &&
      !startswith(lower(topic.name), "goog") &&
      can(regex("^projects/[^/]+/locations/[^/]+/keyRings/[^/]+/cryptoKeys/[^/]+$", topic.kms_key_name)) &&
      topic.message_retention_seconds >= 600 &&
      topic.message_retention_seconds <= 2678400 &&
      floor(topic.message_retention_seconds) == topic.message_retention_seconds &&
      length(topic.allowed_persistence_regions) > 0 &&
      alltrue([
        for region in topic.allowed_persistence_regions :
        can(regex("^[a-z]+(?:-[a-z0-9]+)+$", region))
      ])
    ])
    error_message = "Topics require valid names, full CMEK names, 600-2678400 second retention, and at least one valid persistence region."
  }

  validation {
    condition = alltrue([
      for topic in values(var.topics) : topic.schema == null ? true : (
        ((topic.schema.managed_schema_key != null ? 1 : 0) + (topic.schema.schema_resource_name != null ? 1 : 0) == 1) &&
        contains(["JSON", "BINARY"], topic.schema.encoding) &&
        (
          topic.schema.schema_resource_name == null ||
          can(regex("^projects/[^/]+/schemas/[A-Za-z][A-Za-z0-9._~+%-]{2,254}$", topic.schema.schema_resource_name))
        ) &&
        (
          topic.schema.first_revision_id == null ?
          topic.schema.last_revision_id == null :
          topic.schema.last_revision_id != null
        ) &&
        (
          topic.schema.schema_resource_name == null ||
          (topic.schema.first_revision_id != null && topic.schema.last_revision_id != null)
        ) &&
        (
          topic.schema.first_revision_id == null ? true :
          (length(trimspace(topic.schema.first_revision_id)) > 0 && length(topic.schema.first_revision_id) <= 255)
        ) &&
        (
          topic.schema.last_revision_id == null ? true :
          (length(trimspace(topic.schema.last_revision_id)) > 0 && length(topic.schema.last_revision_id) <= 255)
        )
      )
    ])
    error_message = "Schema settings require exactly one managed or external reference, JSON/BINARY encoding, paired revision bounds, and explicit bounds for external schemas."
  }

  validation {
    condition = alltrue(flatten([
      for topic in values(var.topics) : [
        for member in setunion(topic.publishers, topic.viewers) :
        !contains(["allUsers", "allAuthenticatedUsers"], member) &&
        can(regex("^(user|group|serviceAccount|domain):[^[:space:]]+$|^principal(Set)?://iam[.]googleapis[.]com/.+$", member))
      ]
    ]))
    error_message = "Topic IAM members must be non-empty and must not be public principals."
  }

  validation {
    condition = alltrue(flatten([
      for topic in values(var.topics) : [
        for key, value in topic.labels :
        can(regex("^[a-z][a-z0-9_-]{0,62}$", key)) &&
        can(regex("^$|^[a-z0-9][a-z0-9_-]{0,62}$", value))
      ]
    ]))
    error_message = "Topic labels must be valid lowercase Google Cloud label pairs."
  }

  validation {
    condition     = length(distinct([for topic in values(var.topics) : topic.name])) == length(var.topics)
    error_message = "Topic resource names must be unique."
  }
}

variable "subscriptions" {
  description = "Durable pull subscriptions keyed by stable Terraform identity."
  type = map(object({
    name                          = string
    topic_key                     = string
    ack_deadline_seconds          = optional(number, 30)
    message_retention_seconds     = optional(number, 604800)
    retain_acked_messages         = optional(bool, false)
    expiration_ttl_seconds        = optional(number)
    retry_minimum_backoff_seconds = optional(number, 10)
    retry_maximum_backoff_seconds = optional(number, 600)
    enable_message_ordering       = optional(bool, false)
    enable_exactly_once_delivery  = optional(bool, false)
    filter                        = optional(string, "")
    labels                        = optional(map(string), {})
    subscribers                   = optional(set(string), [])
    viewers                       = optional(set(string), [])
    dead_letter = optional(object({
      topic_key             = string
      max_delivery_attempts = optional(number, 10)
    }))
  }))
  default = {}

  validation {
    condition = alltrue([
      for subscription in values(var.subscriptions) :
      can(regex("^[A-Za-z][A-Za-z0-9._~+%-]{2,254}$", subscription.name)) &&
      !startswith(lower(subscription.name), "goog") &&
      subscription.ack_deadline_seconds >= 10 &&
      subscription.ack_deadline_seconds <= 600 &&
      floor(subscription.ack_deadline_seconds) == subscription.ack_deadline_seconds &&
      subscription.message_retention_seconds >= 600 &&
      subscription.message_retention_seconds <= 2678400 &&
      floor(subscription.message_retention_seconds) == subscription.message_retention_seconds &&
      (
        subscription.expiration_ttl_seconds == null ? true :
        (
          subscription.expiration_ttl_seconds >= 86400 &&
          subscription.expiration_ttl_seconds <= 31536000 &&
          floor(subscription.expiration_ttl_seconds) == subscription.expiration_ttl_seconds
        )
      )
    ])
    error_message = "Subscriptions require valid names, 10-600 second ack deadlines, 600-2678400 second retention, and null or 1-day-to-1-year expiration."
  }

  validation {
    condition = alltrue([
      for subscription in values(var.subscriptions) :
      subscription.retry_minimum_backoff_seconds >= 0 &&
      subscription.retry_minimum_backoff_seconds <= 600 &&
      floor(subscription.retry_minimum_backoff_seconds) == subscription.retry_minimum_backoff_seconds &&
      subscription.retry_maximum_backoff_seconds >= 0 &&
      subscription.retry_maximum_backoff_seconds <= 600 &&
      floor(subscription.retry_maximum_backoff_seconds) == subscription.retry_maximum_backoff_seconds &&
      subscription.retry_minimum_backoff_seconds <= subscription.retry_maximum_backoff_seconds
    ])
    error_message = "Retry backoffs must be whole seconds from 0 through 600, with minimum no greater than maximum."
  }

  validation {
    condition = alltrue([
      for subscription in values(var.subscriptions) :
      length(subscription.filter) <= 256 &&
      (
        subscription.dead_letter == null ? true :
        (
          subscription.dead_letter.max_delivery_attempts >= 5 &&
          subscription.dead_letter.max_delivery_attempts <= 100 &&
          floor(subscription.dead_letter.max_delivery_attempts) == subscription.dead_letter.max_delivery_attempts
        )
      )
    ])
    error_message = "Subscription filters are limited to 256 characters and dead-letter attempts to a whole number from 5 through 100."
  }

  validation {
    condition = alltrue(flatten([
      for subscription in values(var.subscriptions) : [
        for member in setunion(subscription.subscribers, subscription.viewers) :
        !contains(["allUsers", "allAuthenticatedUsers"], member) &&
        can(regex("^(user|group|serviceAccount|domain):[^[:space:]]+$|^principal(Set)?://iam[.]googleapis[.]com/.+$", member))
      ]
    ]))
    error_message = "Subscription IAM members must be non-empty and must not be public principals."
  }

  validation {
    condition = alltrue(flatten([
      for subscription in values(var.subscriptions) : [
        for key, value in subscription.labels :
        can(regex("^[a-z][a-z0-9_-]{0,62}$", key)) &&
        can(regex("^$|^[a-z0-9][a-z0-9_-]{0,62}$", value))
      ]
    ]))
    error_message = "Subscription labels must be valid lowercase Google Cloud label pairs."
  }

  validation {
    condition     = length(distinct([for subscription in values(var.subscriptions) : subscription.name])) == length(var.subscriptions)
    error_message = "Subscription resource names must be unique."
  }
}
