# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

mock_provider "google" {}

variables {
  project_id                 = "mindclade-production"
  environment                = "production"
  owner                      = "data-platform"
  pubsub_service_agent_email = "service-123456789012@gcp-sa-pubsub.iam.gserviceaccount.com"
}

run "hardened_schema_topic_subscription_and_dead_letter_contract" {
  command = plan

  variables {
    schemas = {
      event-v1 = {
        name       = "event-v1"
        type       = "AVRO"
        definition = "{\"type\":\"record\",\"name\":\"Event\",\"fields\":[]}"
      }
    }

    topics = {
      events = {
        name                        = "events"
        kms_key_name                = "projects/security/locations/us/keyRings/data/cryptoKeys/pubsub"
        message_retention_seconds   = 604800
        allowed_persistence_regions = ["us-central1", "us-east1"]
        schema = {
          managed_schema_key = "event-v1"
          encoding           = "JSON"
        }
        publishers = ["serviceAccount:publisher@mindclade-production.iam.gserviceaccount.com"]
      }
      events-dlq = {
        name                        = "events-dlq"
        kms_key_name                = "projects/security/locations/us/keyRings/data/cryptoKeys/pubsub"
        allowed_persistence_regions = ["us-central1", "us-east1"]
      }
    }

    subscriptions = {
      workers = {
        name                          = "events-workers"
        topic_key                     = "events"
        enable_message_ordering       = true
        enable_exactly_once_delivery  = true
        retry_minimum_backoff_seconds = 10
        retry_maximum_backoff_seconds = 600
        subscribers                   = ["serviceAccount:worker@mindclade-production.iam.gserviceaccount.com"]
        dead_letter = {
          topic_key             = "events-dlq"
          max_delivery_attempts = 10
        }
      }
      dlq-operations = {
        name        = "events-dlq-operations"
        topic_key   = "events-dlq"
        subscribers = ["group:platform-oncall@example.com"]
      }
    }
  }

  assert {
    condition = (
      google_pubsub_topic.this["events"].kms_key_name == "projects/security/locations/us/keyRings/data/cryptoKeys/pubsub" &&
      google_pubsub_topic.this["events"].message_retention_duration == "604800s" &&
      google_pubsub_topic.this["events"].message_storage_policy[0].enforce_in_transit == true
    )
    error_message = "Topics must preserve CMEK, bounded retention, and enforced persistence-region transit."
  }

  assert {
    condition     = google_pubsub_topic.this["events"].schema_settings[0].encoding == "JSON"
    error_message = "The topic must enforce its managed schema encoding."
  }

  assert {
    condition = (
      google_pubsub_subscription.this["workers"].enable_message_ordering == true &&
      google_pubsub_subscription.this["workers"].enable_exactly_once_delivery == true &&
      google_pubsub_subscription.this["workers"].retry_policy[0].minimum_backoff == "10s" &&
      google_pubsub_subscription.this["workers"].retry_policy[0].maximum_backoff == "600s" &&
      google_pubsub_subscription.this["workers"].dead_letter_policy[0].max_delivery_attempts == 10
    )
    error_message = "Subscription ordering, exactly-once acknowledgement, retry, and dead-letter settings must remain explicit."
  }

  assert {
    condition = (
      google_pubsub_topic_iam_member.dead_letter_publisher["events-dlq"].role == "roles/pubsub.publisher" &&
      google_pubsub_subscription_iam_member.dead_letter_subscriber["workers"].role == "roles/pubsub.subscriber"
    )
    error_message = "The Pub/Sub service agent needs both resource-scoped dead-letter grants."
  }
}

run "reject_public_topic_principal" {
  command = plan

  variables {
    topics = {
      events = {
        name                        = "events"
        kms_key_name                = "projects/security/locations/us/keyRings/data/cryptoKeys/pubsub"
        allowed_persistence_regions = ["us-central1"]
        publishers                  = ["allUsers"]
      }
    }
  }

  expect_failures = [var.topics]
}

run "reject_unpinned_external_schema" {
  command = plan

  variables {
    topics = {
      events = {
        name                        = "events"
        kms_key_name                = "projects/security/locations/us/keyRings/data/cryptoKeys/pubsub"
        allowed_persistence_regions = ["us-central1"]
        schema = {
          schema_resource_name = "projects/contracts/schemas/event-v1"
          encoding             = "BINARY"
        }
      }
    }
  }

  expect_failures = [var.topics]
}

run "reject_unknown_managed_schema_key" {
  command = plan

  variables {
    topics = {
      events = {
        name                        = "events"
        kms_key_name                = "projects/security/locations/us/keyRings/data/cryptoKeys/pubsub"
        allowed_persistence_regions = ["us-central1"]
        schema = {
          managed_schema_key = "missing"
          encoding           = "JSON"
        }
      }
    }
  }

  expect_failures = [google_pubsub_topic.this["events"]]
}

run "reject_unknown_subscription_topic" {
  command = plan

  variables {
    subscriptions = {
      workers = {
        name      = "events-workers"
        topic_key = "missing"
      }
    }
  }

  expect_failures = [google_pubsub_subscription.this["workers"]]
}

run "reject_dead_letter_topic_without_consumer" {
  command = plan

  variables {
    topics = {
      events = {
        name                        = "events"
        kms_key_name                = "projects/security/locations/us/keyRings/data/cryptoKeys/pubsub"
        allowed_persistence_regions = ["us-central1"]
      }
      events-dlq = {
        name                        = "events-dlq"
        kms_key_name                = "projects/security/locations/us/keyRings/data/cryptoKeys/pubsub"
        allowed_persistence_regions = ["us-central1"]
      }
    }
    subscriptions = {
      workers = {
        name      = "events-workers"
        topic_key = "events"
        dead_letter = {
          topic_key = "events-dlq"
        }
      }
    }
  }

  expect_failures = [google_pubsub_subscription.this["workers"]]
}
