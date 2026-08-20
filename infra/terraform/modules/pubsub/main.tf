# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

locals {
  baseline_labels = {
    environment  = var.environment
    "managed-by" = "terraform"
    owner        = var.owner
  }

  topic_publishers = merge({}, [
    for topic_key, topic in var.topics : {
      for member in topic.publishers :
      "${topic_key}/${sha256(member)}" => {
        topic_key = topic_key
        member    = member
      }
    }
  ]...)

  topic_viewers = merge({}, [
    for topic_key, topic in var.topics : {
      for member in topic.viewers :
      "${topic_key}/${sha256(member)}" => {
        topic_key = topic_key
        member    = member
      }
    }
  ]...)

  schema_viewers = merge({}, [
    for schema_key, schema in var.schemas : {
      for member in schema.viewers :
      "${schema_key}/${sha256(member)}" => {
        schema_key = schema_key
        member     = member
      }
    }
  ]...)

  subscription_subscribers = merge({}, [
    for subscription_key, subscription in var.subscriptions : {
      for member in subscription.subscribers :
      "${subscription_key}/${sha256(member)}" => {
        subscription_key = subscription_key
        member           = member
      }
    }
  ]...)

  subscription_viewers = merge({}, [
    for subscription_key, subscription in var.subscriptions : {
      for member in subscription.viewers :
      "${subscription_key}/${sha256(member)}" => {
        subscription_key = subscription_key
        member           = member
      }
    }
  ]...)

  dead_letter_subscriptions = {
    for key, subscription in var.subscriptions : key => subscription
    if subscription.dead_letter != null
  }
  dead_letter_topic_keys = toset([
    for subscription in values(local.dead_letter_subscriptions) :
    subscription.dead_letter.topic_key
  ])
  service_agent_member = "serviceAccount:${var.pubsub_service_agent_email}"
}

resource "google_pubsub_schema" "this" {
  for_each = var.schemas

  project         = var.project_id
  name            = each.value.name
  type            = each.value.type
  definition      = each.value.definition
  deletion_policy = "PREVENT"

  lifecycle {
    prevent_destroy = true
  }
}

resource "google_pubsub_topic" "this" {
  for_each = var.topics

  project                    = var.project_id
  name                       = each.value.name
  kms_key_name               = each.value.kms_key_name
  message_retention_duration = "${each.value.message_retention_seconds}s"
  labels                     = merge(var.labels, each.value.labels, local.baseline_labels)

  message_storage_policy {
    allowed_persistence_regions = sort(tolist(each.value.allowed_persistence_regions))
    enforce_in_transit          = true
  }

  dynamic "schema_settings" {
    for_each = each.value.schema == null ? [] : [each.value.schema]

    content {
      schema = schema_settings.value.managed_schema_key != null ? try(
        google_pubsub_schema.this[schema_settings.value.managed_schema_key].id,
        "",
      ) : schema_settings.value.schema_resource_name
      encoding = schema_settings.value.encoding
      first_revision_id = schema_settings.value.first_revision_id != null ? schema_settings.value.first_revision_id : (
        schema_settings.value.managed_schema_key == null ? null : try(
          google_pubsub_schema.this[schema_settings.value.managed_schema_key].revision_id,
          null,
        )
      )
      last_revision_id = schema_settings.value.last_revision_id != null ? schema_settings.value.last_revision_id : (
        schema_settings.value.managed_schema_key == null ? null : try(
          google_pubsub_schema.this[schema_settings.value.managed_schema_key].revision_id,
          null,
        )
      )
    }
  }

  lifecycle {
    prevent_destroy = true

    precondition {
      condition = each.value.schema == null ? true : (
        each.value.schema.managed_schema_key == null ||
        contains(keys(var.schemas), each.value.schema.managed_schema_key)
      )
      error_message = "A topic managed_schema_key must identify a schema declared in this module instance."
    }

    precondition {
      condition     = length(merge(var.labels, each.value.labels, local.baseline_labels)) <= 64
      error_message = "Merged topic labels must not exceed the Google Cloud limit of 64 pairs."
    }
  }
}

resource "google_pubsub_subscription" "this" {
  for_each = var.subscriptions

  project                      = var.project_id
  name                         = each.value.name
  topic                        = try(google_pubsub_topic.this[each.value.topic_key].id, "")
  labels                       = merge(var.labels, each.value.labels, local.baseline_labels)
  ack_deadline_seconds         = each.value.ack_deadline_seconds
  message_retention_duration   = "${each.value.message_retention_seconds}s"
  retain_acked_messages        = each.value.retain_acked_messages
  filter                       = each.value.filter
  enable_message_ordering      = each.value.enable_message_ordering
  enable_exactly_once_delivery = each.value.enable_exactly_once_delivery

  expiration_policy {
    ttl = each.value.expiration_ttl_seconds == null ? "" : "${each.value.expiration_ttl_seconds}s"
  }

  retry_policy {
    minimum_backoff = "${each.value.retry_minimum_backoff_seconds}s"
    maximum_backoff = "${each.value.retry_maximum_backoff_seconds}s"
  }

  dynamic "dead_letter_policy" {
    for_each = each.value.dead_letter == null ? [] : [each.value.dead_letter]

    content {
      dead_letter_topic     = try(google_pubsub_topic.this[dead_letter_policy.value.topic_key].id, "")
      max_delivery_attempts = dead_letter_policy.value.max_delivery_attempts
    }
  }

  lifecycle {
    prevent_destroy = true

    precondition {
      condition     = contains(keys(var.topics), each.value.topic_key)
      error_message = "Every subscription topic_key must identify a topic declared in this module instance."
    }

    precondition {
      condition = each.value.dead_letter == null ? true : (
        contains(keys(var.topics), each.value.dead_letter.topic_key) &&
        each.value.dead_letter.topic_key != each.value.topic_key
      )
      error_message = "A dead-letter topic must be a different topic declared in this module instance."
    }

    precondition {
      condition = each.value.dead_letter == null ? true : anytrue([
        for candidate in values(var.subscriptions) :
        candidate.topic_key == each.value.dead_letter.topic_key
      ])
      error_message = "Every dead-letter topic needs at least one subscription or dead-lettered messages can be lost."
    }

    precondition {
      condition     = length(merge(var.labels, each.value.labels, local.baseline_labels)) <= 64
      error_message = "Merged subscription labels must not exceed the Google Cloud limit of 64 pairs."
    }
  }
}

# Additive, resource-scoped IAM only. Authoritative IAM policy/binding resources are
# intentionally excluded because they can remove grants owned by another state.
resource "google_pubsub_topic_iam_member" "publisher" {
  for_each = local.topic_publishers

  project = var.project_id
  topic   = google_pubsub_topic.this[each.value.topic_key].name
  role    = "roles/pubsub.publisher"
  member  = each.value.member
}

resource "google_pubsub_topic_iam_member" "viewer" {
  for_each = local.topic_viewers

  project = var.project_id
  topic   = google_pubsub_topic.this[each.value.topic_key].name
  role    = "roles/pubsub.viewer"
  member  = each.value.member
}

resource "google_pubsub_schema_iam_member" "viewer" {
  for_each = local.schema_viewers

  project = var.project_id
  schema  = google_pubsub_schema.this[each.value.schema_key].name
  role    = "roles/pubsub.viewer"
  member  = each.value.member
}

resource "google_pubsub_subscription_iam_member" "subscriber" {
  for_each = local.subscription_subscribers

  project      = var.project_id
  subscription = google_pubsub_subscription.this[each.value.subscription_key].name
  role         = "roles/pubsub.subscriber"
  member       = each.value.member
}

resource "google_pubsub_subscription_iam_member" "viewer" {
  for_each = local.subscription_viewers

  project      = var.project_id
  subscription = google_pubsub_subscription.this[each.value.subscription_key].name
  role         = "roles/pubsub.viewer"
  member       = each.value.member
}

# Pub/Sub's service agent needs both grants for dead-letter forwarding. Creating only the
# dead_letter_policy block leaves a best-effort policy that cannot forward messages.
resource "google_pubsub_topic_iam_member" "dead_letter_publisher" {
  for_each = local.dead_letter_topic_keys

  project = var.project_id
  topic   = google_pubsub_topic.this[each.value].name
  role    = "roles/pubsub.publisher"
  member  = local.service_agent_member
}

resource "google_pubsub_subscription_iam_member" "dead_letter_subscriber" {
  for_each = local.dead_letter_subscriptions

  project      = var.project_id
  subscription = google_pubsub_subscription.this[each.key].name
  role         = "roles/pubsub.subscriber"
  member       = local.service_agent_member
}
