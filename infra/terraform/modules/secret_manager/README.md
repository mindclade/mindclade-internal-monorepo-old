# Secret Manager module

This module creates deletion-protected Secret Manager metadata containers with
automatic or explicit multi-region replication, optional CMEK, delayed version
destruction, notifications, a rotation schedule, governance labels, and additive
least-privilege IAM. It never accepts or creates a secret payload.

Restricted secrets require CMEK in every selected replica. Payload-access and
version-adder principals must be disjoint. `rotation_period` and
`next_rotation_time` are required together and bounded to the Secret Manager API
window. A plan-time precondition retains the API's five-minute lead time; protected
apply workflows must discard stale saved plans because the plan timestamp remains
stable during apply. Annotations are restricted to printable non-sensitive metadata with API-valid
keys and a byte-exact ASCII size ceiling.

Write payload versions through an approved runtime or rotation workflow so plaintext
does not enter Terraform configuration, plans, logs, or state. Rotation scheduling
only emits a Pub/Sub notification; it requires an independently deployed, monitored,
and idempotent handler. When the module must create the topic,
`rotation_topic_kms_key_name` is mandatory and the topic is deletion-protected;
`required_rotation_topic_kms_grant` reports the additive Pub/Sub service-agent grant.
KMS and caller-supplied topic IAM remain in their owning states to preserve separation
of duties.

Before rollout, validate effective IAM and inheritance, service-agent grants, data
residency, KMS location/availability, rotation and rollback, stale-version disablement,
audit-log routing, access-deny canaries, and emergency access. The
`unexpected_access_alert_intent` output passes the alerting requirement to the
observability state; this IAM/metadata module deliberately does not own paging.
Offline tests prove the metadata policy only and never access a live secret.

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
| <a name="input_alert_on_unexpected_access"></a> [alert\_on\_unexpected\_access](#input\_alert\_on\_unexpected\_access) | Alert when a secret is read by a principal outside its accessor list.<br/><br/>DATA\_READ auditing on secretmanager is enabled org-wide, which is what makes this<br/>detectable at all; this turns the log line into a page. The alert policy itself is created<br/>by the observability unit — this flag is the declaration of intent that unit reads. | `bool` | `true` | no |
| <a name="input_data_classification"></a> [data\_classification](#input\_data\_classification) | public, internal, confidential, or restricted. `restricted` requires CMEK on every replica. | `string` | `"confidential"` | no |
| <a name="input_environment"></a> [environment](#input\_environment) | Environment label applied to every secret | `string` | n/a | yes |
| <a name="input_labels"></a> [labels](#input\_labels) | Labels applied to every secret, merged under each secret's own. | `map(string)` | `{}` | no |
| <a name="input_notification_topics"></a> [notification\_topics](#input\_notification\_topics) | Pub/Sub topics notified on secret events. REQUIRED for any secret carrying a rotation\_period — Secret Manager only emits the rotation event; something else has to act on it. | `set(string)` | `[]` | no |
| <a name="input_owner"></a> [owner](#input\_owner) | Accountable team label | `string` | `"platform"` | no |
| <a name="input_project_id"></a> [project\_id](#input\_project\_id) | Project that owns every secret this module creates | `string` | n/a | yes |
| <a name="input_project_number"></a> [project\_number](#input\_project\_number) | NUMERIC id of var.project\_id.<br/><br/>Required separately because a Workload Identity direct principal is addressed by project<br/>NUMBER while the pool inside it is addressed by project ID — the same project, named two<br/>ways in one string:<br/><br/>  principal://iam.googleapis.com/projects/<NUMBER>/locations/global/<br/>    workloadIdentityPools/<ID>.svc.id.goog/subject/ns/<ns>/sa/<ksa><br/><br/>Substituting the id for the number produces a member string that IAM accepts and no<br/>workload ever matches, so the apply succeeds and every secret read is denied. | `string` | n/a | yes |
| <a name="input_replication"></a> [replication](#input\_replication) | Replication policy, shared by every secret here.<br/><br/>USER-MANAGED, not automatic. Automatic replication puts a copy in every Google region,<br/>which is convenient and violates the residency org policy — a secret is data. Naming the<br/>replica explicitly is also what makes CMEK possible at all: automatic replication cannot<br/>use a customer key. | <pre>object({<br/>    user_managed = optional(list(object({<br/>      location     = string<br/>      kms_key_name = optional(string)<br/>    })), [])<br/><br/>    # Only when user_managed is empty. Kept for completeness; nothing in this estate uses it.<br/>    automatic_kms_key_name = optional(string)<br/>  })</pre> | n/a | yes |
| <a name="input_rotation_topic_kms_key_name"></a> [rotation\_topic\_kms\_key\_name](#input\_rotation\_topic\_kms\_key\_name) | Full CMEK resource name for the module-created rotation topic; unnecessary when notification\_topics supplies pre-governed topics. | `string` | `null` | no |
| <a name="input_secrets"></a> [secrets](#input\_secrets) | Secret CONTAINERS keyed by secret id.<br/><br/>This module never creates a VERSION, and that is the important property rather than an<br/>omission: a secret value passed to Terraform ends up in the state file, in every plan<br/>artifact, and in every local .terraform directory. The containers are declared here; the<br/>values are written out of band, by a human or a rotation job.<br/><br/>`accessors` and `version_adders` name keys from var.workload\_identity\_bindings rather than<br/>IAM member strings, so that "who can read this" is answerable from one file instead of a<br/>search across rendered manifests. | <pre>map(object({<br/>    description = string<br/><br/>    # Keys into var.workload_identity_bindings. Deliberately not raw member strings — a<br/>    # binding declared here and nowhere else is a typo that fails at plan rather than an<br/>    # IAM grant to a principal that does not exist.<br/>    accessors      = optional(set(string), [])<br/>    version_adders = optional(set(string), [])<br/>    viewers        = optional(set(string), [])<br/><br/>    # Seconds, as a string, matching the API. A secret with no rotation period is one nobody<br/>    # will ever rotate — the annotation is what a rotation job reads to know what it owns.<br/>    rotation_period    = optional(string)<br/>    next_rotation_time = optional(string)<br/><br/>    labels      = optional(map(string), {})<br/>    annotations = optional(map(string), {})<br/>  }))</pre> | n/a | yes |
| <a name="input_version_destroy_delay_days"></a> [version\_destroy\_delay\_days](#input\_version\_destroy\_delay\_days) | Delay before a destroyed version is unrecoverable. Non-zero so that destroying the wrong version is survivable. | `number` | `7` | no |
| <a name="input_workload_identity_bindings"></a> [workload\_identity\_bindings](#input\_workload\_identity\_bindings) | Accessor name to the Kubernetes service account that assumes it.<br/><br/>Workloads read secrets through Workload Identity, so no pod holds a credential and nothing<br/>is mounted from a file. The chain terminates at a service account token GKE mints per pod,<br/>which cannot be exfiltrated usefully: it expires in an hour and is bound to one service<br/>account in one namespace. | <pre>map(object({<br/>    namespace       = string<br/>    service_account = string<br/>  }))</pre> | `{}` | no |

## Outputs

| Name | Description |
| ---- | ----------- |
| <a name="output_required_rotation_topic_kms_grant"></a> [required\_rotation\_topic\_kms\_grant](#output\_required\_rotation\_topic\_kms\_grant) | Additive grant the key-owning state must apply before the module creates a CMEK-protected rotation topic; null for caller-supplied topics. |
| <a name="output_rotating_secret_ids"></a> [rotating\_secret\_ids](#output\_rotating\_secret\_ids) | Secrets carrying a rotation period.<br/><br/>Exported so a rotation job can enumerate what it owns rather than being configured with a<br/>list that drifts from this one. |
| <a name="output_secret_ids"></a> [secret\_ids](#output\_secret\_ids) | Secret id by map key. |
| <a name="output_secret_names"></a> [secret\_names](#output\_secret\_names) | Fully qualified secret names, for a CSI SecretProviderClass or a workload's own config. |
| <a name="output_unexpected_access_alert_intent"></a> [unexpected\_access\_alert\_intent](#output\_unexpected\_access\_alert\_intent) | Declarative input for the observability state that owns unexpected Secret Manager DATA\_READ alerting. |
<!-- END_TF_DOCS -->
