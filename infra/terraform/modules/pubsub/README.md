# Pub/Sub module

This module creates private, CMEK-encrypted Pub/Sub schemas, topics, and durable pull
subscriptions. Topics have explicit message retention and persistence regions.
Subscriptions have bounded acknowledgement deadlines, retention, expiration, retry
backoff, filters, dead-letter attempts, ordering, and Pub/Sub exactly-once-delivery
settings. Resources are deletion protected.

IAM is additive and resource scoped. Only fixed publisher, subscriber, and viewer roles
are exposed, and public principals are rejected. The module grants the Pub/Sub service
agent publisher access to managed dead-letter topics and subscriber access to each source
subscription. A dead-letter topic must have its own subscription. Dead-letter delivery is
still a best-effort service behavior; alert on undelivered-message and oldest-unacked-age
metrics and test replay.

Every topic requires a CMEK. The key-owning state must grant the service agent the role
reported by `required_kms_grants`; this module deliberately does not take ownership of KMS
IAM. Older projects can have an additional Pub/Sub service-agent token-creator prerequisite,
which must be checked in the live project.

## Schema contract

A topic can reference a schema created in the same module or a fully qualified external
schema. Managed schemas default both allowed revision bounds to the revision Terraform
created. External references must provide both revision IDs. Schema type and encoding are
validated, but Terraform cannot prove that an AVRO or Protobuf definition is semantically
valid before the Pub/Sub API checks it. The conservative definition bound is measured in
Terraform characters, while the API and subscription-filter limits are byte-oriented; keep
definitions and filters ASCII when operating at those limits.

Exactly-once delivery is a Pub/Sub acknowledgement guarantee for a message ID, not
end-to-end business exactly once. Publishers can publish duplicate logical events with
different IDs. Consumers still require inbox/idempotency keys and digest-conflict checks,
matching the repository's transactional inbox architecture. The guarantee is regional:
subscribers must use one region (and a locational endpoint when necessary); multi-region
subscriber connections can still receive duplicates.

```hcl
module "events" {
  source = "../../modules/pubsub"

  project_id                 = "mindclade-production"
  environment                = "production"
  owner                      = "data-platform"
  pubsub_service_agent_email = "service-123456789012@gcp-sa-pubsub.iam.gserviceaccount.com"

  schemas = {
    event-v1 = {
      name       = "event-v1"
      type       = "AVRO"
      definition = file("${path.root}/schemas/event-v1.avsc")
    }
  }

  topics = {
    events = {
      name                        = "events"
      kms_key_name                = "projects/security/locations/us/keyRings/data/cryptoKeys/pubsub"
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
      name                         = "events-workers"
      topic_key                    = "events"
      enable_message_ordering      = true
      enable_exactly_once_delivery = true
      subscribers                  = ["serviceAccount:worker@mindclade-production.iam.gserviceaccount.com"]
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
```

Run formatting, initialization with the repository lock policy, validation, and
`terraform test` before reviewing a saved plan. Mock-provider tests prove Terraform
contracts only; they do not prove API enablement, service-agent creation, KMS IAM,
quota, metrics, delivery, replay, or regional exactly-once behavior.
