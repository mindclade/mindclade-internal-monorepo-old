# Immutable Compute Image module

Creates one deletion-protected, CMEK-encrypted Compute Engine image from a create-only Cloud
Storage raw-disk archive. The object URL must contain its SHA-256 digest; its GCS generation is
retained in the resource description and output so release evidence can prove the exact import.

The module does not create the artifact bucket, upload an image, grant publisher access, or select
the image for a VM. Those remain separate storage, release-workflow, KMS-owner, and caller-state
responsibilities. It also creates no image family: consumers use the exact `self_link` output.

Provider-backed tests validate content addressing, CMEK, and deletion safeguards without creating
cloud resources. Connected qualification must confirm the source object's generation/digest, the
image's `READY` status, Shielded VM boot, and a zero-change follow-up plan.

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
| <a name="input_compute_service_account_email"></a> [compute\_service\_account\_email](#input\_compute\_service\_account\_email) | Compute Engine service-agent email authorized on kms\_key\_name | `string` | n/a | yes |
| <a name="input_data_classification"></a> [data\_classification](#input\_data\_classification) | Image governance classification; public is forbidden | `string` | `"internal"` | no |
| <a name="input_description"></a> [description](#input\_description) | Non-sensitive purpose included before immutable source evidence | `string` | `"Mindclade immutable NixOS workstation image."` | no |
| <a name="input_environment"></a> [environment](#input\_environment) | Environment governance label | `string` | n/a | yes |
| <a name="input_image_contract_sha256"></a> [image\_contract\_sha256](#input\_image\_contract\_sha256) | Lowercase SHA-256 digest of the contract embedded in the source image | `string` | n/a | yes |
| <a name="input_kms_key_name"></a> [kms\_key\_name](#input\_kms\_key\_name) | CMEK crypto-key resource name encrypting the Compute Image | `string` | n/a | yes |
| <a name="input_labels"></a> [labels](#input\_labels) | Additional labels; baseline governance labels take precedence | `map(string)` | `{}` | no |
| <a name="input_name"></a> [name](#input\_name) | Immutable image name; callers create a new name for every artifact digest | `string` | n/a | yes |
| <a name="input_owner"></a> [owner](#input\_owner) | Accountable team governance label | `string` | n/a | yes |
| <a name="input_project_id"></a> [project\_id](#input\_project\_id) | Google Cloud project that owns the immutable Compute Image | `string` | n/a | yes |
| <a name="input_qualification_state"></a> [qualification\_state](#input\_qualification\_state) | Explicit cross-repository evidence transition authorizing Compute Image creation | `string` | n/a | yes |
| <a name="input_source_bucket_name"></a> [source\_bucket\_name](#input\_source\_bucket\_name) | Exact Terraform-owned Cloud Storage bucket from which the raw disk may be imported | `string` | n/a | yes |
| <a name="input_source_object_generation"></a> [source\_object\_generation](#input\_source\_object\_generation) | Generation of the create-only GCS source object retained as release evidence | `string` | n/a | yes |
| <a name="input_source_sha256"></a> [source\_sha256](#input\_source\_sha256) | Lowercase SHA-256 digest of the compressed raw-disk artifact | `string` | n/a | yes |
| <a name="input_source_uri"></a> [source\_uri](#input\_source\_uri) | Full HTTPS Cloud Storage URL of a disk.raw tar.gz object whose name contains its SHA-256 | `string` | n/a | yes |
| <a name="input_storage_locations"></a> [storage\_locations](#input\_storage\_locations) | Locations where Compute Engine stores the encrypted image | `list(string)` | <pre>[<br/>  "us"<br/>]</pre> | no |

## Outputs

| Name | Description |
| ---- | ----------- |
| <a name="output_encryption_contract"></a> [encryption\_contract](#output\_encryption\_contract) | CMEK identity and external grant required by the image import |
| <a name="output_image"></a> [image](#output\_image) | Immutable Compute Image identity |
| <a name="output_source_contract"></a> [source\_contract](#output\_source\_contract) | Create-only artifact identity that must match release provenance |
<!-- END_TF_DOCS -->
