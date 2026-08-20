# Immutable audit archive composition

This module composes `../log_sink` into one central organization- or folder-level Cloud
Audit Logs archive. It captures Admin Activity, System Event, Policy Denied, and Data
Access log IDs from the parent and all descendants, with no exclusions or caller-supplied
narrowing filter.

The GCS destination enforces uniform access, public-access prevention, versioning,
90-day soft delete by default, CMEK, `force_destroy = false`, provider deletion policy,
Terraform `prevent_destroy`, and a locked retention policy. Storage-class transitions
reduce long-term cost but never delete audit objects. Server-access records are routed to
a distinct pre-existing bucket so access to the evidence archive is itself reviewable.

Bucket Lock is irreversible: retention cannot later be shortened or removed, and changing
location requires a new bucket. The caller must supply the exact confirmation only after
security, legal, recovery, and cost owners approve the name, location, key, and retention.
A migration must overlap old and new sinks and verify delivery before retiring anything.

This module routes logs that Google Cloud emits; it does not enable Data Access audit logs
for individual services. Manage those audit configs at the organization/folder/project IAM
authority and verify expected log types with canary activity. It also cannot prove delivery:
alert on sink errors, periodically compare expected versus archived entries, verify the
unique writer's object-creator grant, and test evidence retrieval.

`destination_project_default_retention_days` affects only the central destination
project's own `_Default` bucket. It cannot change every descendant project's local bucket
and defaults to zero (unmanaged).

```hcl
module "audit_archive" {
  source = "../../modules/audit_archive"

  parent                      = "organizations/123456789012"
  project_id                  = "mindclade-audit"
  environment                 = "production"
  owner                       = "security"
  storage_service_agent_email = "service-123456789012@gs-project-accounts.iam.gserviceaccount.com"
  bucket_name                 = "mindclade-central-audit-archive"
  access_log_bucket_name      = "mindclade-central-storage-access"
  location                    = "US"
  kms_key_name                = "projects/mindclade-security/locations/us/keyRings/audit/cryptoKeys/archive"

  retention_lock_confirmation = "LOCKING A CLOUD STORAGE RETENTION POLICY IS IRREVERSIBLE"
}
```

The Cloud Storage service agent needs access to the archive CMEK, and the Storage analytics
group needs object-creator on the external access-log bucket. Both required additive grants
are exported while their IAM remains in the owning states; verify them before apply.
Mock-provider tests do not prove audit-config enablement, KMS compatibility, VPC Service
Controls behavior, log delivery, retention-law suitability, or evidence recovery.

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
| <a name="input_access_log_bucket_name"></a> [access\_log\_bucket\_name](#input\_access\_log\_bucket\_name) | Existing separately governed bucket receiving server-access logs for the audit archive. | `string` | n/a | yes |
| <a name="input_access_log_object_prefix"></a> [access\_log\_object\_prefix](#input\_access\_log\_object\_prefix) | Non-sensitive object prefix for audit-archive server-access logs. | `string` | `"audit-archive/"` | no |
| <a name="input_bucket_name"></a> [bucket\_name](#input\_bucket\_name) | Globally unique archive bucket name. | `string` | n/a | yes |
| <a name="input_destination_project_default_retention_days"></a> [destination\_project\_default\_retention\_days](#input\_destination\_project\_default\_retention\_days) | Retention for only the central destination project's \_Default bucket; 0 leaves it unmanaged. | `number` | `0` | no |
| <a name="input_environment"></a> [environment](#input\_environment) | Environment governance label for the central archive. | `string` | n/a | yes |
| <a name="input_kms_key_name"></a> [kms\_key\_name](#input\_kms\_key\_name) | Full CMEK resource name in a location compatible with the archive bucket. | `string` | n/a | yes |
| <a name="input_labels"></a> [labels](#input\_labels) | Additional archive labels; security baseline labels take precedence. | `map(string)` | `{}` | no |
| <a name="input_location"></a> [location](#input\_location) | Archive bucket region, dual-region, or multi-region. | `string` | n/a | yes |
| <a name="input_owner"></a> [owner](#input\_owner) | Accountable security or platform team label. | `string` | n/a | yes |
| <a name="input_parent"></a> [parent](#input\_parent) | Organization or folder covered by the aggregated audit sink. | `string` | n/a | yes |
| <a name="input_project_id"></a> [project\_id](#input\_project\_id) | Dedicated central logging project that owns the archive bucket. | `string` | n/a | yes |
| <a name="input_retention_days"></a> [retention\_days](#input\_retention\_days) | Irreversible minimum audit-object retention once the bucket is created and locked. | `number` | `2555` | no |
| <a name="input_retention_lock_confirmation"></a> [retention\_lock\_confirmation](#input\_retention\_lock\_confirmation) | Required exact acknowledgement of the irreversible Bucket Lock operation. | `string` | n/a | yes |
| <a name="input_sink_name"></a> [sink\_name](#input\_sink\_name) | Stable aggregated sink name. | `string` | `"central-audit-archive"` | no |
| <a name="input_soft_delete_retention_days"></a> [soft\_delete\_retention\_days](#input\_soft\_delete\_retention\_days) | Additional recovery window for deleted objects, from 7 through 90 days. | `number` | `90` | no |
| <a name="input_storage_service_agent_email"></a> [storage\_service\_agent\_email](#input\_storage\_service\_agent\_email) | Google-managed Cloud Storage service-agent email for project\_id, used to report the required archive CMEK grant. | `string` | n/a | yes |

## Outputs

| Name | Description |
| ---- | ----------- |
| <a name="output_audit_contract"></a> [audit\_contract](#output\_audit\_contract) | Reviewable immutable audit controls and coverage intent. |
| <a name="output_bucket_name"></a> [bucket\_name](#output\_bucket\_name) | Central immutable audit archive bucket name. |
| <a name="output_required_access_log_writer_grant"></a> [required\_access\_log\_writer\_grant](#output\_required\_access\_log\_writer\_grant) | Additive grant the separately governed access-log bucket state must apply. |
| <a name="output_required_kms_grant"></a> [required\_kms\_grant](#output\_required\_kms\_grant) | Additive grant the archive CryptoKey-owning state must apply. |
| <a name="output_sink_id"></a> [sink\_id](#output\_sink\_id) | Aggregated organization or folder sink ID. |
| <a name="output_writer_identity"></a> [writer\_identity](#output\_writer\_identity) | Unique sink writer identity granted append-only access to the archive. |
<!-- END_TF_DOCS -->
