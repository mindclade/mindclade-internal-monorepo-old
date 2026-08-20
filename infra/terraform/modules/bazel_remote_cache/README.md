# Bazel remote-cache bucket module

This module composes `../storage` into a private, CMEK-encrypted Cloud Storage
backend for Bazel remote caching. The cache is explicitly **rebuildable**: source
control and pinned build inputs remain authoritative, while cached action results
and content-addressed blobs only reduce build latency.

Uniform bucket-level access, enforced public-access prevention, versioning, soft
delete, a minimum retention period, server-access logging, additive IAM member
grants, `force_destroy = false`, and both Terraform and provider deletion guards
are inherited from the storage module. A required CMEK must share the bucket
location. Live entries expire after `cache_ttl_days`; old noncurrent generations
expire sooner, and incomplete multipart uploads are aborted after one day. The
retention period cannot exceed the live-entry TTL and is deliberately not locked.
`data_classification` accepts `internal`, `confidential`, or `restricted` so build
outputs retain an explicit governance label; the bucket remains private in every
case and public classification is forbidden.

Readers receive `roles/storage.objectViewer`. Cache identities in `writer_members`
receive `roles/storage.objectCreator` and viewer access, but never object-admin
access; lifecycle policy, not the executor, owns deletion. Publishers must use
content-addressed keys and `ifGenerationMatch=0` so a cache entry cannot be
overwritten. Keep the writer set limited to workload identities; no public
principal is accepted. These are additive IAM grants and do not replace unrelated
bucket IAM policy. Human reader access uses groups, never direct user or domain
grants, and wildcard principals are rejected. A cache implementation that requires
overwrites or ad hoc deletion is outside this hardened contract and needs a
separate security review.

## Integration and operations

The separately governed logging bucket must grant
`group:cloud-storage-analytics@google.com` `roles/storage.objectCreator`; the exact
grant is returned by `required_access_log_writer_grant`. Grant the Cloud Storage
service agent `roles/cloudkms.cryptoKeyEncrypterDecrypter` on the selected key
outside this module. Configure clients for authenticated HTTPS or `gs://` access,
TLS verification, bounded retries, and digest verification. Never publish cache
URLs as anonymous endpoints or treat cache hits as a provenance decision.

Budget for versioning, soft-delete, access-log, KMS, request, and egress costs.
Cloud Storage evaluates both lifecycle and retention policy, so an entry becomes
eligible at its configured TTL but deletion can be delayed until retention permits
it. Monitor cache hit ratio, latency, error rate, object count/bytes, lifecycle deletion,
KMS errors, authorization failures, and access-log delivery. Test cache loss and a
cold rebuild before production. Decommissioning requires a reviewed code change to
remove deletion guards, waiting for retention constraints, and separately handling
the CMEK and access-log records.

Provider-mock tests prove configuration contracts and input rejection only; they
do not prove bucket-name availability, cloud IAM propagation, logging delivery,
KMS permissions, cache correctness, or build reproducibility.

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
| <a name="input_access_log_object_prefix"></a> [access\_log\_object\_prefix](#input\_access\_log\_object\_prefix) | Non-sensitive prefix for this cache's access-log objects | `string` | `"bazel-remote-cache/"` | no |
| <a name="input_bucket_name"></a> [bucket\_name](#input\_bucket\_name) | Globally unique bucket name | `string` | n/a | yes |
| <a name="input_cache_ttl_days"></a> [cache\_ttl\_days](#input\_cache\_ttl\_days) | Age at which rebuildable live cache entries are deleted | `number` | `14` | no |
| <a name="input_data_classification"></a> [data\_classification](#input\_data\_classification) | Governance classification for cached build outputs; public classification is forbidden | `string` | `"internal"` | no |
| <a name="input_environment"></a> [environment](#input\_environment) | Environment governance label | `string` | n/a | yes |
| <a name="input_kms_key_name"></a> [kms\_key\_name](#input\_kms\_key\_name) | CMEK CryptoKey resource name in the bucket location | `string` | n/a | yes |
| <a name="input_labels"></a> [labels](#input\_labels) | Additional labels; cache and storage governance labels take precedence | `map(string)` | `{}` | no |
| <a name="input_location"></a> [location](#input\_location) | Cloud Storage region, dual-region, or multi-region | `string` | n/a | yes |
| <a name="input_noncurrent_version_ttl_days"></a> [noncurrent\_version\_ttl\_days](#input\_noncurrent\_version\_ttl\_days) | Age at which noncurrent cache generations are deleted while retaining one newer generation | `number` | `1` | no |
| <a name="input_owner"></a> [owner](#input\_owner) | Accountable team governance label | `string` | n/a | yes |
| <a name="input_project_id"></a> [project\_id](#input\_project\_id) | Project that owns the Bazel remote-cache bucket | `string` | n/a | yes |
| <a name="input_reader_members"></a> [reader\_members](#input\_reader\_members) | Workload identities and groups granted additive objectViewer access | `set(string)` | `[]` | no |
| <a name="input_retention_period_seconds"></a> [retention\_period\_seconds](#input\_retention\_period\_seconds) | Minimum object-retention period; bounded below the cache TTL and deliberately not locked | `number` | `86400` | no |
| <a name="input_soft_delete_retention_days"></a> [soft\_delete\_retention\_days](#input\_soft\_delete\_retention\_days) | Recovery window after cache-object deletion | `number` | `7` | no |
| <a name="input_writer_members"></a> [writer\_members](#input\_writer\_members) | Non-public cache identities granted additive create-only plus read access | `set(string)` | n/a | yes |

## Outputs

| Name | Description |
| ---- | ----------- |
| <a name="output_bucket"></a> [bucket](#output\_bucket) | Hardened Bazel remote-cache bucket resource |
| <a name="output_cache_policy"></a> [cache\_policy](#output\_cache\_policy) | Reviewable rebuildable-cache durability and deletion contract |
| <a name="output_gs_uri"></a> [gs\_uri](#output\_gs\_uri) | Cloud Storage URI for cache backend configuration |
| <a name="output_https_uri"></a> [https\_uri](#output\_https\_uri) | Authenticated HTTPS endpoint prefix; this is not a public URL |
| <a name="output_iam_contract"></a> [iam\_contract](#output\_iam\_contract) | Additive non-public bucket IAM contract; cache writers are also effective readers |
| <a name="output_kms_key_name"></a> [kms\_key\_name](#output\_kms\_key\_name) | Default CMEK CryptoKey |
| <a name="output_required_access_log_writer_grant"></a> [required\_access\_log\_writer\_grant](#output\_required\_access\_log\_writer\_grant) | Additive grant the separately owned access-log bucket must implement |
<!-- END_TF_DOCS -->
