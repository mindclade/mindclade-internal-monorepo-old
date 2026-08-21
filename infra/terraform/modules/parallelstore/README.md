# Parallelstore module

Owns typed, deletion-protected Parallelstore scratch or persistent instances. GCS import intent is
reported as a qualification contract because the reviewed Google provider exposes no Terraform
resource for Parallelstore imports; a protected connected transfer workflow must execute and record
that step.

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
| <a name="input_labels"></a> [labels](#input\_labels) | n/a | `map(string)` | `{}` | no |
| <a name="input_parallelstore"></a> [parallelstore](#input\_parallelstore) | Parallelstore scratch instances keyed by stable identity. | <pre>map(object({<br/>    name              = string<br/>    location          = string<br/>    capacity_gib      = number<br/>    deployment_type   = string<br/>    network           = string<br/>    reserved_ip_range = string<br/>    gcs_import = optional(object({<br/>      source = string<br/>    }))<br/>  }))</pre> | n/a | yes |
| <a name="input_project_id"></a> [project\_id](#input\_project\_id) | n/a | `string` | n/a | yes |

## Outputs

| Name | Description |
| ---- | ----------- |
| <a name="output_gcs_import_contracts"></a> [gcs\_import\_contracts](#output\_gcs\_import\_contracts) | Explicit post-create imports; execution requires a separately reviewed transfer workflow because the provider exposes no import resource. |
| <a name="output_instances"></a> [instances](#output\_instances) | n/a |
<!-- END_TF_DOCS -->
