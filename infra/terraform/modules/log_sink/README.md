# Aggregated log sink module

This module creates aggregated sinks on either an organization or a folder and routes each
sink to a dedicated Cloud Logging or GCS bucket in `project_id`. Organization and folder
sinks use their respective provider resources; a `folders/...` parent is never passed as
an organization ID.

Every sink requests a unique writer identity. The module then adds only the destination
grant that identity needs: conditional Logging bucket-writer for a Cloud Logging
destination or GCS object-creator for an archive. A configured sink is not proof of
delivery, so monitor sink errors and verify expected canary entries.

Cloud Logging destinations have bounded retention, configurable location, deletion policy,
and Terraform destruction protection. Log Analytics is a creation-time decision. GCS
destinations enforce uniform access, public-access prevention, versioning, soft delete,
`force_destroy = false`, provider deletion policy, and Terraform destruction protection.
CMEK is optional at this generic layer; the key-owning state must grant the relevant
service agent. Every GCS destination also requires a distinct, separately governed
`access_log_bucket_name`; `required_access_log_writer_grants` reports the additive
Cloud Storage analytics grant that bucket's owning state must apply. Keep both buckets
in compatible locations, organizations, and VPC Service Controls perimeters.

GCS retention is distinct from lifecycle deletion. Retention locking is irreversible and
requires both `lock_retention_policy = true` and the exact
`retention_lock_confirmation`. Generic archives default to an unlocked policy; the
`audit_archive` composition requires a locked policy.

`default_sink_retention_days` manages only the destination project's own global
`_Default` bucket. It cannot update the local buckets of descendant projects. The
backward-compatible default is 30 days; set it to zero to leave that bucket unmanaged.

```hcl
module "logs" {
  source = "../../modules/log_sink"

  parent     = "folders/123456789012"
  project_id = "mindclade-logging"

  sinks = {
    application-hot = {
      description      = "Queryable application logs."
      destination      = "logging"
      filter           = "resource.type=\"k8s_container\""
      retention_days   = 30
      enable_analytics = true
    }
  }
}
```

Mock-provider tests validate resource selection and configuration only. They do not prove
Logging API enablement, IAM propagation, CMEK access, destination compatibility, live
delivery, exclusions, retention execution, cost, or recovery.

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
| <a name="input_default_sink_retention_days"></a> [default\_sink\_retention\_days](#input\_default\_sink\_retention\_days) | Retention for only project\_id's own \_Default log bucket; 0 leaves that bucket unmanaged. Descendant project buckets are outside this module's scope. | `number` | `30` | no |
| <a name="input_include_children"></a> [include\_children](#input\_include\_children) | Include descendant folders and projects in the aggregated sink. | `bool` | `true` | no |
| <a name="input_labels"></a> [labels](#input\_labels) | Labels applied to GCS archive buckets. Cloud Logging buckets do not carry labels. | `map(string)` | `{}` | no |
| <a name="input_logging_bucket_location"></a> [logging\_bucket\_location](#input\_logging\_bucket\_location) | Location for Cloud Logging destinations; fixed when each bucket is created. | `string` | `"global"` | no |
| <a name="input_parent"></a> [parent](#input\_parent) | Organization or folder on which to create aggregated sinks, as organizations/<id> or folders/<id>. | `string` | n/a | yes |
| <a name="input_project_id"></a> [project\_id](#input\_project\_id) | Project that owns destination buckets and their billing/perimeter boundary. | `string` | n/a | yes |
| <a name="input_retention_lock_confirmation"></a> [retention\_lock\_confirmation](#input\_retention\_lock\_confirmation) | Exact irreversible-action acknowledgement required when any GCS archive retention policy is locked. | `string` | `null` | no |
| <a name="input_sinks"></a> [sinks](#input\_sinks) | Aggregated sinks keyed by a stable sink name. "logging" creates a queryable Cloud<br/>Logging bucket. "storage" creates a deletion-protected GCS archive bucket. A storage<br/>retention lock is irreversible and additionally requires retention\_lock\_confirmation. | <pre>map(object({<br/>    description = string<br/>    destination = string<br/>    filter      = string<br/><br/>    enable_analytics = optional(bool, false)<br/>    retention_days   = optional(number, 30)<br/><br/>    bucket = optional(object({<br/>      name                       = string<br/>      location                   = string<br/>      access_log_bucket_name     = string<br/>      access_log_object_prefix   = optional(string, "log-archive/")<br/>      encryption_key             = optional(string)<br/>      retention_days             = optional(number)<br/>      lock_retention_policy      = optional(bool, false)<br/>      soft_delete_retention_days = optional(number, 30)<br/>      lifecycle_rules = optional(list(object({<br/>        age           = number<br/>        action        = string<br/>        storage_class = optional(string)<br/>      })), [])<br/>    }))<br/><br/>    exclusions = optional(list(object({<br/>      name        = string<br/>      description = optional(string, "")<br/>      filter      = string<br/>    })), [])<br/>  }))</pre> | n/a | yes |

## Outputs

| Name | Description |
| ---- | ----------- |
| <a name="output_analytics_enabled_buckets"></a> [analytics\_enabled\_buckets](#output\_analytics\_enabled\_buckets) | Cloud Logging destinations configured with Log Analytics. |
| <a name="output_default_bucket_scope"></a> [default\_bucket\_scope](#output\_default\_bucket\_scope) | The only \_Default bucket this module can manage; null means it is left unmanaged. |
| <a name="output_log_bucket_ids"></a> [log\_bucket\_ids](#output\_log\_bucket\_ids) | Cloud Logging bucket IDs keyed by sink name. |
| <a name="output_required_access_log_writer_grants"></a> [required\_access\_log\_writer\_grants](#output\_required\_access\_log\_writer\_grants) | Additive grants required on separately governed Cloud Storage access-log buckets. |
| <a name="output_sink_ids"></a> [sink\_ids](#output\_sink\_ids) | Aggregated sink resource IDs keyed by sink name, independent of parent type. |
| <a name="output_storage_bucket_names"></a> [storage\_bucket\_names](#output\_storage\_bucket\_names) | GCS archive bucket names keyed by sink name. |
| <a name="output_storage_kms_key_names"></a> [storage\_kms\_key\_names](#output\_storage\_kms\_key\_names) | Configured GCS archive CMEK names, excluding archives that use Google-managed encryption. |
| <a name="output_writer_identities"></a> [writer\_identities](#output\_writer\_identities) | Unique service account used by each sink, keyed by sink name. |
<!-- END_TF_DOCS -->
