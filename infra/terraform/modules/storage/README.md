# Cloud Storage module

This module creates one private, deletion-protected bucket. Uniform bucket-level
access, public-access prevention, versioning, and a 30-day soft-delete window are
secure defaults. IAM grants are additive and object-scoped; public principals are
rejected. Every bucket configures a separately governed server-access-log destination.
The log-bucket-owning state must grant `roles/storage.objectCreator` to
`group:cloud-storage-analytics@google.com`; the module exports that required grant but
does not take ownership of a shared bucket from each source-bucket state. Before apply,
verify that both buckets satisfy Cloud Storage's location, organization, and VPC
Service Controls requirements. A configured destination is not proof that logs arrive.
Optional CMEK and lifecycle rules remain explicit inputs.

`create_only_workload = true` adds the NOVA training checkpoint boundary: at
least one creator must also be a viewer, objectAdmin is forbidden, versioning
is required, and lifecycle policy may only abort incomplete multipart uploads.
This Terraform guard cannot enforce request preconditions. The object client
must still use `ifGenerationMatch=0`, stream and verify checksums, upload every
rank shard, and publish the digest-bound manifest last. A generation returned
by Cloud Storage is the committed artifact identity.

The module contains no objects or secrets and does not grant the Cloud Storage
service agent access to a supplied KMS key. Establish that cross-service grant in
the key-owning state to preserve separation of duties. Retention locking requires
an exact acknowledgement because it is irreversible.

```hcl
module "artifacts" {
  source = "../../modules/storage"

  project_id          = "mindclade-development"
  name                = "mindclade-development-artifacts"
  location            = "US-CENTRAL1"
  access_log_bucket   = "mindclade-central-storage-logs"
  environment         = "development"
  owner               = "cloud-platform"
  data_classification = "restricted"
}
```

Validate with `terraform init -backend=false`, `terraform validate`, and
`terraform test`. A passing offline test is not evidence that IAM, KMS, quotas,
replication, alerting, or restore exercises are correct in a live project.

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
| <a name="input_access_log_bucket"></a> [access\_log\_bucket](#input\_access\_log\_bucket) | Existing separately governed bucket receiving server-access logs; null is allowed only for a dedicated access-log sink | `string` | `null` | no |
| <a name="input_access_log_object_prefix"></a> [access\_log\_object\_prefix](#input\_access\_log\_object\_prefix) | Non-sensitive prefix for access-log objects | `string` | `"storage-access/"` | no |
| <a name="input_create_only_workload"></a> [create\_only\_workload](#input\_create\_only\_workload) | Enforce the additive IAM/lifecycle boundary used by manifest-last NOVA training checkpoint publication; clients must still send ifGenerationMatch=0 | `bool` | `false` | no |
| <a name="input_data_classification"></a> [data\_classification](#input\_data\_classification) | Data-classification governance label | `string` | `"confidential"` | no |
| <a name="input_environment"></a> [environment](#input\_environment) | Environment governance label | `string` | n/a | yes |
| <a name="input_kms_key_name"></a> [kms\_key\_name](#input\_kms\_key\_name) | Optional CryptoKey resource name; grant the Storage service agent encrypt/decrypt separately | `string` | `null` | no |
| <a name="input_labels"></a> [labels](#input\_labels) | Additional labels; baseline governance labels take precedence | `map(string)` | `{}` | no |
| <a name="input_lifecycle_rules"></a> [lifecycle\_rules](#input\_lifecycle\_rules) | Cost and retention lifecycle rules; keep deletion decisions explicit | <pre>list(object({<br/>    action                     = string<br/>    storage_class              = optional(string)<br/>    age_days                   = optional(number)<br/>    days_since_noncurrent_time = optional(number)<br/>    matches_prefix             = optional(list(string))<br/>    matches_suffix             = optional(list(string))<br/>    num_newer_versions         = optional(number)<br/>    with_state                 = optional(string)<br/>  }))</pre> | <pre>[<br/>  {<br/>    "action": "AbortIncompleteMultipartUpload",<br/>    "age_days": 7,<br/>    "storage_class": null,<br/>    "with_state": null<br/>  }<br/>]</pre> | no |
| <a name="input_location"></a> [location](#input\_location) | Region, dual-region, or multi-region for the bucket | `string` | n/a | yes |
| <a name="input_lock_retention_policy"></a> [lock\_retention\_policy](#input\_lock\_retention\_policy) | Permanently lock the retention policy; irreversible | `bool` | `false` | no |
| <a name="input_name"></a> [name](#input\_name) | Globally unique bucket name | `string` | n/a | yes |
| <a name="input_object_admins"></a> [object\_admins](#input\_object\_admins) | IAM members allowed to manage objects; use sparingly | `set(string)` | `[]` | no |
| <a name="input_object_creators"></a> [object\_creators](#input\_object\_creators) | IAM members allowed to create, but not overwrite or delete, objects | `set(string)` | `[]` | no |
| <a name="input_object_viewers"></a> [object\_viewers](#input\_object\_viewers) | IAM members allowed to read objects | `set(string)` | `[]` | no |
| <a name="input_owner"></a> [owner](#input\_owner) | Accountable team governance label | `string` | n/a | yes |
| <a name="input_project_id"></a> [project\_id](#input\_project\_id) | Project that owns the bucket | `string` | n/a | yes |
| <a name="input_retention_lock_confirmation"></a> [retention\_lock\_confirmation](#input\_retention\_lock\_confirmation) | Exact irreversible-action acknowledgement required when lock\_retention\_policy is true | `string` | `null` | no |
| <a name="input_retention_period_seconds"></a> [retention\_period\_seconds](#input\_retention\_period\_seconds) | Optional minimum object retention period; null disables Bucket Lock policy | `number` | `null` | no |
| <a name="input_soft_delete_retention_days"></a> [soft\_delete\_retention\_days](#input\_soft\_delete\_retention\_days) | Soft-delete recovery window; Cloud Storage supports 7-90 days | `number` | `30` | no |
| <a name="input_storage_class"></a> [storage\_class](#input\_storage\_class) | Default storage class | `string` | `"STANDARD"` | no |
| <a name="input_versioning_enabled"></a> [versioning\_enabled](#input\_versioning\_enabled) | Retain noncurrent object generations | `bool` | `true` | no |

## Outputs

| Name | Description |
| ---- | ----------- |
| <a name="output_bucket"></a> [bucket](#output\_bucket) | Bucket resource |
| <a name="output_kms_key_name"></a> [kms\_key\_name](#output\_kms\_key\_name) | Configured default CryptoKey, if any |
| <a name="output_required_access_log_writer_grant"></a> [required\_access\_log\_writer\_grant](#output\_required\_access\_log\_writer\_grant) | Additive IAM grant the separately owned access-log bucket must implement |
<!-- END_TF_DOCS -->
