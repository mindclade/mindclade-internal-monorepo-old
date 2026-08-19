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
