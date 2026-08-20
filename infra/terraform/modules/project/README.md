# GCP project module

This module is the project-factory contract used below the organization and
folder foundation state. It creates one guarded project, disables default-network
creation, attaches billing, applies baseline labels, enables selected APIs, and
optionally adds a project budget and resource-manager tag bindings.

It optionally attaches the project to a Shared VPC host project as a service
project, and optionally deprivileges the default compute service account. Both
are off by default: attaching every project by default would attach the one
project deliberately kept outside the workload network, and nothing at the call
site would say so.

The default service account is DEPRIVILEGED, never deleted. Deletion is
recoverable for 30 days and permanent after that, and a later workload that
legitimately needs the account fails with an error naming a missing account
rather than a missing role — a much harder thing to diagnose than a denied
permission.

The module does not create a Google Cloud organization, folders, tag keys or tag
values, billing accounts, notification channels, IAM grants, or authoritative
Data Access audit-log configuration. Those are separate lifecycle and privilege
boundaries. The hierarchy IAM state owns audit configuration once; project promotion
must verify that the inherited policy covers every required service and log type.

Every project is protected by both `deletion_policy = "PREVENT"` and Terraform
`prevent_destroy`. API services remain enabled during destroy. A project must be
explicitly migrated out of this contract before an approved deletion workflow.
Project IDs and names follow the Resource Manager API grammar, including its
restricted ID strings. Budget email routing accepts at most five full Cloud
Monitoring email-channel resource names; the owning monitoring state must create
and validate those channels before this module is applied. Global project-ID
uniqueness can only be confirmed by the live Resource Manager API.

```hcl
module "application_project" {
  source = "../../modules/project"

  project_id         = "mindclade-development"
  project_name       = "Mindclade development"
  folder_id          = "folders/123456789012"
  billing_account_id = "ABCDEF-012345-6789AB"
  environment        = "development"
  owner              = "cloud-platform"

  activate_apis = [
    "logging.googleapis.com",
    "monitoring.googleapis.com",
  ]

  monthly_budget_usd = 500
  tag_value_names    = ["tagValues/234567890123"]

  shared_vpc_host_project_id     = "mindclade-development-net"
  remove_default_service_account = true
}
```

This is a reusable baseline, not a production-ready landing zone. Callers remain
responsible for IAM, organization-policy inheritance, centralized logging, alert
routing, quota, and workload-specific controls.

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
| <a name="input_activate_apis"></a> [activate\_apis](#input\_activate\_apis) | Google APIs to enable without disabling them during destroy | `set(string)` | `[]` | no |
| <a name="input_billing_account_id"></a> [billing\_account\_id](#input\_billing\_account\_id) | Billing account attached to the project | `string` | n/a | yes |
| <a name="input_budget_notification_channels"></a> [budget\_notification\_channels](#input\_budget\_notification\_channels) | Monitoring notification channel resource names for budget updates | `set(string)` | `[]` | no |
| <a name="input_data_classification"></a> [data\_classification](#input\_data\_classification) | Data-classification label applied to the project | `string` | `"internal"` | no |
| <a name="input_enable_project_level_budget_recipients"></a> [enable\_project\_level\_budget\_recipients](#input\_enable\_project\_level\_budget\_recipients) | Notify project owners through the billing budget | `bool` | `true` | no |
| <a name="input_environment"></a> [environment](#input\_environment) | Environment label applied to the project | `string` | n/a | yes |
| <a name="input_folder_id"></a> [folder\_id](#input\_folder\_id) | Parent folder resource name, for example folders/123456789012 | `string` | n/a | yes |
| <a name="input_labels"></a> [labels](#input\_labels) | Additional GCP labels; baseline governance labels take precedence | `map(string)` | `{}` | no |
| <a name="input_monthly_budget_usd"></a> [monthly\_budget\_usd](#input\_monthly\_budget\_usd) | Optional whole-dollar monthly project budget; null disables budget creation | `number` | `null` | no |
| <a name="input_owner"></a> [owner](#input\_owner) | Accountable team label applied to the project | `string` | n/a | yes |
| <a name="input_project_id"></a> [project\_id](#input\_project\_id) | Globally unique GCP project ID | `string` | n/a | yes |
| <a name="input_project_name"></a> [project\_name](#input\_project\_name) | Human-readable GCP project name | `string` | n/a | yes |
| <a name="input_remove_default_service_account"></a> [remove\_default\_service\_account](#input\_remove\_default\_service\_account) | Deprivilege the default compute service account by removing its automatic project IAM grants; the account itself is retained | `bool` | `false` | no |
| <a name="input_resource_lifecycle"></a> [resource\_lifecycle](#input\_resource\_lifecycle) | Lifecycle label applied to the project | `string` | `"persistent"` | no |
| <a name="input_shared_vpc_host_project_id"></a> [shared\_vpc\_host\_project\_id](#input\_shared\_vpc\_host\_project\_id) | Shared VPC host project to attach this project to as a service project; null leaves it unattached | `string` | `null` | no |
| <a name="input_tag_value_names"></a> [tag\_value\_names](#input\_tag\_value\_names) | Namespaced tag value resource names to bind to the project | `set(string)` | `[]` | no |

## Outputs

| Name | Description |
| ---- | ----------- |
| <a name="output_budget_name"></a> [budget\_name](#output\_budget\_name) | Billing budget resource name, or null when budgets are disabled |
| <a name="output_enabled_services"></a> [enabled\_services](#output\_enabled\_services) | Google APIs managed for the project |
| <a name="output_folder_id"></a> [folder\_id](#output\_folder\_id) | Parent folder resource name |
| <a name="output_project_id"></a> [project\_id](#output\_project\_id) | Created GCP project ID |
| <a name="output_project_name"></a> [project\_name](#output\_project\_name) | Created GCP project name |
| <a name="output_project_number"></a> [project\_number](#output\_project\_number) | Created GCP project number |
| <a name="output_shared_vpc_host_project_id"></a> [shared\_vpc\_host\_project\_id](#output\_shared\_vpc\_host\_project\_id) | Shared VPC host project this project is attached to, or null when unattached |
<!-- END_TF_DOCS -->
