# Object storage composition

This is an opinionated multi-bucket composition of `../storage`; it does not declare a
second bucket implementation. The sibling storage module remains the single authority for
bucket resources, deletion protection, uniform access, public-access prevention,
versioning, soft delete, server-access logging, retention, lifecycle, CMEK, and additive
object IAM.

The composition creates three explicit trust classes:

- one restricted access-log bucket, retained for at least a year and transitioned through
  colder storage without lifecycle deletion;
- mutable governed data buckets classified as raw, curated, reference, dataset, or
  evidence;
- restricted create-only AI artifact buckets classified as checkpoint, model, evaluation,
  or release evidence.

AI publishers receive creator plus viewer, never object-admin, and the underlying module
permits only incomplete-multipart cleanup. Clients must still upload with
`ifGenerationMatch=0`, verify checksums/digests, and publish a digest-bound manifest last.
Bucket controls cannot enforce that application protocol.

All managed buckets require CMEK and are protected with provider deletion policy plus
Terraform `prevent_destroy`. KMS IAM stays in the key-owning state; the composition
exports the required Cloud Storage service-agent grants. The managed access-log
bucket grants only object-creator to Cloud Storage's analytics writer. It must itself log to
a separately governed upstream bucket to avoid a self-logging cycle; the required upstream
grant is exported. Before apply, verify that source and logging buckets meet Cloud Storage
location, organization, and VPC Service Controls requirements.

```hcl
module "object_storage" {
  source = "../../modules/object_storage"

  project_id                     = "mindclade-production"
  environment                    = "production"
  owner                          = "data-platform"
  storage_service_agent_email    = "service-123456789012@gs-project-accounts.iam.gserviceaccount.com"
  upstream_access_log_bucket_name = "mindclade-central-storage-access-logs"

  access_log_bucket = {
    name         = "mindclade-production-storage-access"
    location     = "US"
    kms_key_name = "projects/security/locations/us/keyRings/data/cryptoKeys/storage"
  }

  data_buckets = {
    curated = {
      name          = "mindclade-production-curated"
      location      = "US"
      kms_key_name  = "projects/security/locations/us/keyRings/data/cryptoKeys/storage"
      data_class    = "curated"
      readers       = ["group:data-consumers@example.com"]
      writers       = ["serviceAccount:curator@mindclade-production.iam.gserviceaccount.com"]
    }
  }

  ai_artifact_buckets = {
    checkpoints = {
      name           = "mindclade-production-checkpoints"
      location       = "US"
      kms_key_name   = "projects/security/locations/us/keyRings/data/cryptoKeys/storage"
      artifact_class = "checkpoint"
      publishers     = ["serviceAccount:trainer@mindclade-production.iam.gserviceaccount.com"]
      readers        = ["serviceAccount:runtime@mindclade-production.iam.gserviceaccount.com"]
    }
  }
}
```

Changing a bucket key or name can imply replacement, while deletion guards intentionally
stop the operation. Use an approved migration with copied-and-verified data and a
state-safe address move; do not disable the guards casually. Mock-provider tests do not
prove KMS grants, access-log delivery, object recovery, legal holds, lifecycle execution,
or restore objectives in a live project.

<!-- BEGIN_TF_DOCS -->
## Requirements

| Name | Version |
| ---- | ------- |
| <a name="requirement_terraform"></a> [terraform](#requirement\_terraform) | >= 1.9.0, < 2.0.0 |
| <a name="requirement_google"></a> [google](#requirement\_google) | >= 7.41.0, < 8.0.0 |

## Providers

No providers.

## Inputs

| Name | Description | Type | Default | Required |
| ---- | ----------- | ---- | ------- | :------: |
| <a name="input_access_log_bucket"></a> [access\_log\_bucket](#input\_access\_log\_bucket) | Managed bucket receiving server-access logs from all data and AI artifact buckets. | <pre>object({<br/>    name                       = string<br/>    location                   = string<br/>    kms_key_name               = string<br/>    storage_class              = optional(string, "STANDARD")<br/>    retention_days             = optional(number, 365)<br/>    soft_delete_retention_days = optional(number, 30)<br/>    viewers                    = optional(set(string), [])<br/>    labels                     = optional(map(string), {})<br/>  })</pre> | n/a | yes |
| <a name="input_ai_artifact_buckets"></a> [ai\_artifact\_buckets](#input\_ai\_artifact\_buckets) | Create-only AI artifact buckets keyed by stable Terraform identity. | <pre>map(object({<br/>    name                       = string<br/>    location                   = string<br/>    kms_key_name               = string<br/>    artifact_class             = string<br/>    storage_class              = optional(string, "STANDARD")<br/>    soft_delete_retention_days = optional(number, 30)<br/>    retention_period_seconds   = optional(number, 7776000)<br/>    publishers                 = set(string)<br/>    readers                    = optional(set(string), [])<br/>    labels                     = optional(map(string), {})<br/>  }))</pre> | `{}` | no |
| <a name="input_data_buckets"></a> [data\_buckets](#input\_data\_buckets) | Governed mutable data buckets keyed by stable Terraform identity. | <pre>map(object({<br/>    name                       = string<br/>    location                   = string<br/>    kms_key_name               = string<br/>    data_class                 = string<br/>    storage_class              = optional(string, "STANDARD")<br/>    data_classification        = optional(string, "restricted")<br/>    soft_delete_retention_days = optional(number, 30)<br/>    retention_period_seconds   = optional(number)<br/>    readers                    = optional(set(string), [])<br/>    writers                    = optional(set(string), [])<br/>    admins                     = optional(set(string), [])<br/>    labels                     = optional(map(string), {})<br/>    lifecycle_rules = optional(list(object({<br/>      action                     = string<br/>      storage_class              = optional(string)<br/>      age_days                   = optional(number)<br/>      days_since_noncurrent_time = optional(number)<br/>      matches_prefix             = optional(list(string))<br/>      matches_suffix             = optional(list(string))<br/>      num_newer_versions         = optional(number)<br/>      with_state                 = optional(string)<br/>      })), [{<br/>      action        = "AbortIncompleteMultipartUpload"<br/>      age_days      = 7<br/>      storage_class = null<br/>      with_state    = null<br/>    }])<br/>  }))</pre> | `{}` | no |
| <a name="input_environment"></a> [environment](#input\_environment) | Environment governance label. | `string` | n/a | yes |
| <a name="input_labels"></a> [labels](#input\_labels) | Additional labels shared by every bucket; class and storage baseline labels take precedence. | `map(string)` | `{}` | no |
| <a name="input_owner"></a> [owner](#input\_owner) | Accountable team governance label. | `string` | n/a | yes |
| <a name="input_project_id"></a> [project\_id](#input\_project\_id) | Project that owns all composed buckets. | `string` | n/a | yes |
| <a name="input_storage_service_agent_email"></a> [storage\_service\_agent\_email](#input\_storage\_service\_agent\_email) | Google-managed Cloud Storage service-agent email for project\_id, used to report required CMEK grants. | `string` | n/a | yes |
| <a name="input_upstream_access_log_bucket_name"></a> [upstream\_access\_log\_bucket\_name](#input\_upstream\_access\_log\_bucket\_name) | Separately governed existing bucket that receives access logs for this composition's access-log bucket. | `string` | n/a | yes |

## Outputs

| Name | Description |
| ---- | ----------- |
| <a name="output_access_log_bucket"></a> [access\_log\_bucket](#output\_access\_log\_bucket) | Managed access-log bucket identity and fixed safeguards. |
| <a name="output_ai_artifact_buckets"></a> [ai\_artifact\_buckets](#output\_ai\_artifact\_buckets) | Create-only AI artifact bucket identities keyed by stable caller key. |
| <a name="output_data_buckets"></a> [data\_buckets](#output\_data\_buckets) | Governed data bucket identities keyed by stable caller key. |
| <a name="output_kms_key_names"></a> [kms\_key\_names](#output\_kms\_key\_names) | CMEK names by bucket class for key-IAM and rotation verification. |
| <a name="output_required_kms_grants"></a> [required\_kms\_grants](#output\_required\_kms\_grants) | Additive grants the KMS-owning state must apply for the Cloud Storage service agent. |
| <a name="output_required_upstream_access_log_writer_grant"></a> [required\_upstream\_access\_log\_writer\_grant](#output\_required\_upstream\_access\_log\_writer\_grant) | Grant required in the separately owned upstream access-log bucket state. |
<!-- END_TF_DOCS -->
