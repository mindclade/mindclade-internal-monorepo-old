# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

output "topics" {
  description = "Topic identities keyed by stable caller key."
  value = {
    for key, topic in google_pubsub_topic.this : key => {
      id   = topic.id
      name = topic.name
    }
  }
}

output "subscriptions" {
  description = "Subscription identities and delivery settings keyed by stable caller key."
  value = {
    for key, subscription in google_pubsub_subscription.this : key => {
      id                        = subscription.id
      name                      = subscription.name
      topic_key                 = var.subscriptions[key].topic_key
      message_ordering          = var.subscriptions[key].enable_message_ordering
      exactly_once_delivery     = var.subscriptions[key].enable_exactly_once_delivery
      message_retention_seconds = var.subscriptions[key].message_retention_seconds
    }
  }
}

output "schemas" {
  description = "Managed schema identities and current revisions keyed by stable caller key."
  value = {
    for key, schema in google_pubsub_schema.this : key => {
      id          = schema.id
      name        = schema.name
      revision_id = schema.revision_id
    }
  }
}

output "required_kms_grants" {
  description = "Additive grants the KMS-owning state must apply; this module does not own CryptoKey IAM."
  value = {
    for key, topic in var.topics : key => {
      crypto_key = topic.kms_key_name
      member     = "serviceAccount:${var.pubsub_service_agent_email}"
      role       = "roles/cloudkms.cryptoKeyEncrypterDecrypter"
    }
  }
}

output "dead_letter_contracts" {
  description = "Dead-letter routes and service-agent permissions configured by this module."
  value = {
    for key, subscription in local.dead_letter_subscriptions : key => {
      source_subscription   = google_pubsub_subscription.this[key].id
      dead_letter_topic     = google_pubsub_topic.this[subscription.dead_letter.topic_key].id
      max_delivery_attempts = subscription.dead_letter.max_delivery_attempts
      service_agent         = local.service_agent_member
    }
  }
}
