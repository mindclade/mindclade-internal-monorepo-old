# Backup and disaster-recovery module

Creates a Backup for GKE plan plus independently encrypted cross-region replica buckets and hourly
or daily Storage Transfer jobs. Callers supply a region-matched, externally governed CMEK; the
module does not create a second key authority. Deletes are never propagated to replicas. The module emits namespace restore
exclusions separately because the provider's backup-plan API can include all namespaces but cannot
express an exclusion list; protected restore automation must enforce that contract.

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
| <a name="input_bucket_replication"></a> [bucket\_replication](#input\_bucket\_replication) | n/a | <pre>map(object({<br/>    source_bucket                              = string<br/>    destination_bucket                         = string<br/>    destination_region                         = string<br/>    kms_key_name                               = string<br/>    delete_objects_unique_in_sink              = bool<br/>    delete_objects_from_source_after_transfer  = bool<br/>    overwrite_objects_already_existing_in_sink = bool<br/>    schedule                                   = string<br/>    retention_days                             = number<br/>  }))</pre> | n/a | yes |
| <a name="input_gke_backup"></a> [gke\_backup](#input\_gke\_backup) | n/a | <pre>object({<br/>    plan_name           = string<br/>    cluster             = string<br/>    location            = string<br/>    cron_schedule       = string<br/>    all_namespaces      = bool<br/>    excluded_namespaces = set(string)<br/>    include_volume_data = bool<br/>    include_secrets     = bool<br/>    encryption_key      = string<br/>    retention = object({<br/>      backup_retain_days      = number<br/>      backup_delete_lock_days = number<br/>    })<br/>  })</pre> | n/a | yes |
| <a name="input_labels"></a> [labels](#input\_labels) | n/a | `map(string)` | `{}` | no |
| <a name="input_project_id"></a> [project\_id](#input\_project\_id) | n/a | `string` | n/a | yes |

## Outputs

| Name | Description |
| ---- | ----------- |
| <a name="output_backup_plan"></a> [backup\_plan](#output\_backup\_plan) | n/a |
| <a name="output_replica_buckets"></a> [replica\_buckets](#output\_replica\_buckets) | n/a |
| <a name="output_restore_exclusion_contract"></a> [restore\_exclusion\_contract](#output\_restore\_exclusion\_contract) | Namespaces that protected restore automation must omit; the provider backup API has no exclusion field. |
| <a name="output_transfer_jobs"></a> [transfer\_jobs](#output\_transfer\_jobs) | n/a |
<!-- END_TF_DOCS -->
