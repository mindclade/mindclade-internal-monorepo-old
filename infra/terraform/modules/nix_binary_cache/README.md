# Nix binary-cache bucket module

This module composes `../storage` into a private, CMEK-encrypted Cloud Storage
backend for immutable Nix binary publication. Unlike the Bazel remote cache, this
bucket is not modeled as disposable performance data: a published store path and
its `.narinfo` metadata are an **immutable artifact contract** and must remain
reproducible, signed, and recoverable.

Uniform bucket-level access, enforced public-access prevention, versioning, soft
delete, a minimum retention period, server-access logging, additive IAM member
grants, `force_destroy = false`, and both Terraform and provider deletion guards
are inherited from the storage module. A required CMEK must share the bucket
location. No lifecycle rule deletes or transitions live or noncurrent objects;
only incomplete multipart uploads are aborted after seven days.
`data_classification` accepts `internal`, `confidential`, or `restricted` so
published artifacts retain an explicit governance label; the bucket remains
private in every case and public classification is forbidden.

Publishers receive `roles/storage.objectCreator` and
`roles/storage.objectViewer`, never object-admin access. Publication tooling must
also send `ifGenerationMatch=0`; IAM alone cannot make an object key immutable.
Reader workload identities and groups receive additive
`roles/storage.objectViewer`. Public, domain, direct-user, and wildcard principals
are rejected. Nix signing keys are intentionally outside Terraform state: sign NAR
metadata in a hardened build/signing service, distribute only the trusted public
key, verify signatures on every client, and publish payloads before metadata.

## Integration and operations

The separately governed logging bucket must grant
`group:cloud-storage-analytics@google.com` `roles/storage.objectCreator`; the exact
grant is returned by `required_access_log_writer_grant`. Grant the Cloud Storage
service agent `roles/cloudkms.cryptoKeyEncrypterDecrypter` on the selected key
outside this module. Authenticated HTTPS use may require client credential/header
integration; never interpret the returned substituter URI as anonymous access.

Retention lock is optional and off by default. Enabling it is irreversible and
requires the exact acknowledgement. Review legal retention, recovery, KMS
cryptoperiod and key-destruction controls, storage growth, and decommissioning
before locking. Budget for retained generations, soft delete, KMS, access logs,
requests, and egress. Monitor publication failures, generation-precondition
failures, signature verification, KMS errors, authorization failures, log delivery,
object count, bytes, and restore exercises.

Provider-mock tests prove configuration contracts and input rejection only; they
do not prove bucket-name availability, IAM/KMS propagation, signing correctness,
client authentication, artifact reproducibility, or recovery procedures.

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
| <a name="input_access_log_bucket"></a> [access\_log\_bucket](#input\_access\_log\_bucket) | Existing separately governed bucket that receives server-access logs | `string` | n/a | yes |
| <a name="input_access_log_object_prefix"></a> [access\_log\_object\_prefix](#input\_access\_log\_object\_prefix) | Non-sensitive prefix for this cache's access-log objects | `string` | `"nix-binary-cache/"` | no |
| <a name="input_bucket_name"></a> [bucket\_name](#input\_bucket\_name) | Globally unique bucket name | `string` | n/a | yes |
| <a name="input_data_classification"></a> [data\_classification](#input\_data\_classification) | Governance classification for published binary artifacts; public classification is forbidden | `string` | `"internal"` | no |
| <a name="input_environment"></a> [environment](#input\_environment) | Environment governance label | `string` | n/a | yes |
| <a name="input_kms_key_name"></a> [kms\_key\_name](#input\_kms\_key\_name) | CMEK CryptoKey resource name in the bucket location | `string` | n/a | yes |
| <a name="input_labels"></a> [labels](#input\_labels) | Additional labels; cache and storage governance labels take precedence | `map(string)` | `{}` | no |
| <a name="input_location"></a> [location](#input\_location) | Cloud Storage region, dual-region, or multi-region | `string` | n/a | yes |
| <a name="input_lock_retention_policy"></a> [lock\_retention\_policy](#input\_lock\_retention\_policy) | Permanently lock the retention policy; irreversible and disabled by default | `bool` | `false` | no |
| <a name="input_owner"></a> [owner](#input\_owner) | Accountable team governance label | `string` | n/a | yes |
| <a name="input_project_id"></a> [project\_id](#input\_project\_id) | Project that owns the Nix binary-cache bucket | `string` | n/a | yes |
| <a name="input_reader_members"></a> [reader\_members](#input\_reader\_members) | Workload identities and groups granted additive objectViewer access | `set(string)` | `[]` | no |
| <a name="input_retention_lock_confirmation"></a> [retention\_lock\_confirmation](#input\_retention\_lock\_confirmation) | Exact irreversible-action acknowledgement required only when locking retention | `string` | `null` | no |
| <a name="input_retention_period_seconds"></a> [retention\_period\_seconds](#input\_retention\_period\_seconds) | Minimum retention for every published binary-cache object | `number` | `2592000` | no |
| <a name="input_soft_delete_retention_days"></a> [soft\_delete\_retention\_days](#input\_soft\_delete\_retention\_days) | Recovery window after a binary-cache object is deleted | `number` | `30` | no |
| <a name="input_writer_members"></a> [writer\_members](#input\_writer\_members) | Non-public publisher identities granted create-only plus read access | `set(string)` | n/a | yes |

## Outputs

| Name | Description |
| ---- | ----------- |
| <a name="output_bucket"></a> [bucket](#output\_bucket) | Hardened Nix binary-cache bucket resource |
| <a name="output_gs_uri"></a> [gs\_uri](#output\_gs\_uri) | Cloud Storage URI used by authenticated publication tooling |
| <a name="output_iam_contract"></a> [iam\_contract](#output\_iam\_contract) | Additive non-public bucket IAM contract; publishers are also effective readers |
| <a name="output_immutable_policy"></a> [immutable\_policy](#output\_immutable\_policy) | Reviewable immutable-publication durability and deletion contract |
| <a name="output_kms_key_name"></a> [kms\_key\_name](#output\_kms\_key\_name) | Default CMEK CryptoKey |
| <a name="output_required_access_log_writer_grant"></a> [required\_access\_log\_writer\_grant](#output\_required\_access\_log\_writer\_grant) | Additive grant the separately owned access-log bucket must implement |
| <a name="output_substituter_uri"></a> [substituter\_uri](#output\_substituter\_uri) | Authenticated HTTPS Nix substituter URI; this is not a public URL |
<!-- END_TF_DOCS -->
