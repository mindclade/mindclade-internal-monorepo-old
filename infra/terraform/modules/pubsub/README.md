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

<!-- BEGIN_TF_DOCS -->
## Requirements

| Name | Version |
| ---- | ------- |
| <a name="requirement_terraform"></a> [terraform](#requirement\_terraform) | >= 1.9.0, < 2.0.0 |
| <a name="requirement_google"></a> [google](#requirement\_google) | >= 7.41.0, < 8.0.0 |

## Providers

| Name | Version |
| ---- | ------- |
| <a name="provider_google"></a> [google](#provider\_google) | >= 7.41.0, < 8.0.0 |

## Inputs

| Name | Description | Type | Default | Required |
| ---- | ----------- | ---- | ------- | :------: |
| <a name="input_environment"></a> [environment](#input\_environment) | Environment governance label. | `string` | n/a | yes |
| <a name="input_labels"></a> [labels](#input\_labels) | Additional labels; baseline environment, owner, and managed-by labels take precedence. | `map(string)` | `{}` | no |
| <a name="input_owner"></a> [owner](#input\_owner) | Accountable team governance label. | `string` | n/a | yes |
| <a name="input_project_id"></a> [project\_id](#input\_project\_id) | Project that owns the Pub/Sub resources. | `string` | n/a | yes |
| <a name="input_pubsub_service_agent_email"></a> [pubsub\_service\_agent\_email](#input\_pubsub\_service\_agent\_email) | Google-managed Pub/Sub service-agent email for project\_id, used for CMEK and dead-letter contracts. | `string` | n/a | yes |
| <a name="input_schemas"></a> [schemas](#input\_schemas) | Schemas keyed by stable Terraform identity. Topics can pin the managed revision. | <pre>map(object({<br/>    name       = string<br/>    type       = string<br/>    definition = string<br/>    viewers    = optional(set(string), [])<br/>  }))</pre> | `{}` | no |
| <a name="input_subscriptions"></a> [subscriptions](#input\_subscriptions) | Durable pull subscriptions keyed by stable Terraform identity. | <pre>map(object({<br/>    name                          = string<br/>    topic_key                     = string<br/>    ack_deadline_seconds          = optional(number, 30)<br/>    message_retention_seconds     = optional(number, 604800)<br/>    retain_acked_messages         = optional(bool, false)<br/>    expiration_ttl_seconds        = optional(number)<br/>    retry_minimum_backoff_seconds = optional(number, 10)<br/>    retry_maximum_backoff_seconds = optional(number, 600)<br/>    enable_message_ordering       = optional(bool, false)<br/>    enable_exactly_once_delivery  = optional(bool, false)<br/>    filter                        = optional(string, "")<br/>    labels                        = optional(map(string), {})<br/>    subscribers                   = optional(set(string), [])<br/>    viewers                       = optional(set(string), [])<br/>    dead_letter = optional(object({<br/>      topic_key             = string<br/>      max_delivery_attempts = optional(number, 10)<br/>    }))<br/>  }))</pre> | `{}` | no |
| <a name="input_topics"></a> [topics](#input\_topics) | CMEK-encrypted topics with bounded retention, residency, optional schema enforcement, and additive IAM. | <pre>map(object({<br/>    name                        = string<br/>    kms_key_name                = string<br/>    message_retention_seconds   = optional(number, 604800)<br/>    allowed_persistence_regions = set(string)<br/>    labels                      = optional(map(string), {})<br/>    publishers                  = optional(set(string), [])<br/>    viewers                     = optional(set(string), [])<br/>    schema = optional(object({<br/>      managed_schema_key   = optional(string)<br/>      schema_resource_name = optional(string)<br/>      encoding             = optional(string, "JSON")<br/>      first_revision_id    = optional(string)<br/>      last_revision_id     = optional(string)<br/>    }))<br/>  }))</pre> | `{}` | no |

## Outputs

| Name | Description |
| ---- | ----------- |
| <a name="output_dead_letter_contracts"></a> [dead\_letter\_contracts](#output\_dead\_letter\_contracts) | Dead-letter routes and service-agent permissions configured by this module. |
| <a name="output_required_kms_grants"></a> [required\_kms\_grants](#output\_required\_kms\_grants) | Additive grants the KMS-owning state must apply; this module does not own CryptoKey IAM. |
| <a name="output_schemas"></a> [schemas](#output\_schemas) | Managed schema identities and current revisions keyed by stable caller key. |
| <a name="output_subscriptions"></a> [subscriptions](#output\_subscriptions) | Subscription identities and delivery settings keyed by stable caller key. |
| <a name="output_topics"></a> [topics](#output\_topics) | Topic identities keyed by stable caller key. |
<!-- END_TF_DOCS -->
