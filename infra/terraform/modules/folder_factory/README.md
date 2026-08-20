# `folder_factory`

Creates folders under an organization or another folder, and raises a monthly budget against
each one that asks for it.

A folder is where org policy and IAM are inherited from, so adding one is a governance change
rather than a filing decision. Two consequences are built into the interface:

- **The map key is the identity, not the display name.** Terraform destroys and recreates a
  folder whose key changes, and the replacement arrives empty of every policy and binding the
  original had. Rename the `display_name`; leave the key alone.
- **`deletion_protection` defaults to `true`.** A folder is opted *out* explicitly, in the
  caller, where the decision is visible in review.

Budgets filter by `resource_ancestors`, never by a project list. A project list goes stale the
moment another project is created in the folder, and a budget that has silently stopped
covering half its folder reads exactly like a folder that got cheaper. Every budget also
carries a `FORECASTED_SPEND` rule alongside the actual-spend thresholds — an alert that first
fires at 100% of actual spend arrives when the money is already gone.

Folder-scoped org policy belongs in the `org_policy` module, not here. Two units declaring a
policy on the same folder each see the other's value as drift and revert it on every apply —
a fight that produces no error and no stable state.

## Usage

```hcl
module "folders" {
  source = "git::https://github.com/mindclade-org/mindclade.git//infra/terraform/modules/folder_factory?ref=v0.2.0"

  parent          = "organizations/123456789012"
  billing_account = "01A2B3-C4D5E6-F70819"

  folders = {
    partners = { display_name = "Partners" }
    sandbox  = { display_name = "Sandbox", deletion_protection = false }
  }

  folder_budgets = {
    partners = 5000
    sandbox  = 2000
  }
}
```

`billing_account` is required whenever `folder_budgets` is non-empty. Google reports its
absence as a permission error rather than a missing field, so a precondition catches it here
instead.

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
| <a name="input_billing_account"></a> [billing\_account](#input\_billing\_account) | Billing account the folder budgets are raised against. Required whenever folder\_budgets<br/>is non-empty — a budget has no meaning without one, and Google reports the omission as a<br/>permission error rather than a missing field. | `string` | `""` | no |
| <a name="input_budget_currency"></a> [budget\_currency](#input\_budget\_currency) | ISO 4217 currency for folder budgets. Must match the billing account's currency. | `string` | `"USD"` | no |
| <a name="input_budget_monitoring_channels"></a> [budget\_monitoring\_channels](#input\_budget\_monitoring\_channels) | Monitoring notification channel IDs alerted on every folder budget threshold. | `list(string)` | `[]` | no |
| <a name="input_budget_threshold_percents"></a> [budget\_threshold\_percents](#input\_budget\_threshold\_percents) | Fractions of the budget at which an alert fires. 1.0 is included deliberately: an alert<br/>that only fires at 90% tells nobody the month actually went over. | `list(number)` | <pre>[<br/>  0.5,<br/>  0.9,<br/>  1<br/>]</pre> | no |
| <a name="input_folder_budgets"></a> [folder\_budgets](#input\_folder\_budgets) | Monthly budget in whole currency units, keyed by the same short name as folders. A folder<br/>with no entry has no budget, which is a decision rather than a default: an unbudgeted<br/>folder is where a runaway spends unnoticed. | `map(number)` | `{}` | no |
| <a name="input_folders"></a> [folders](#input\_folders) | Folders to create, keyed by a stable short name. The key is the identity: renaming it<br/>destroys and recreates the folder, taking every IAM binding and org policy attached to<br/>it, so the display name is what changes when a folder is renamed. | <pre>map(object({<br/>    display_name        = string<br/>    deletion_protection = optional(bool, true)<br/>  }))</pre> | n/a | yes |
| <a name="input_parent"></a> [parent](#input\_parent) | Resource under which every folder is created, as organizations/<id> or folders/<id> | `string` | n/a | yes |

## Outputs

| Name | Description |
| ---- | ----------- |
| <a name="output_budget_ids"></a> [budget\_ids](#output\_budget\_ids) | Billing budget resource ids keyed by folder short name. |
| <a name="output_folder_ids"></a> [folder\_ids](#output\_folder\_ids) | Folder resource ids as folders/<numeric id>, keyed by the short name. This is the form every downstream parent field expects. |
| <a name="output_folder_names"></a> [folder\_names](#output\_folder\_names) | Display names as created, keyed by short name. |
| <a name="output_folder_numbers"></a> [folder\_numbers](#output\_folder\_numbers) | Bare numeric folder ids, keyed by short name, for the APIs that reject the folders/ prefix. |
<!-- END_TF_DOCS -->
