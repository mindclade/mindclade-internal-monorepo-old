# Organization governance module

This module owns a protected organization tag taxonomy, bindings of those tags to
organization/folder/project resources, and narrowly validated additive
organization IAM grants. It does not create or discover the organization.

## Security contract

- IAM is managed only with `google_organization_iam_member`. The module never
  uses policy- or role-authoritative IAM resources.
- Basic roles, roles whose ID ends in `Admin`, service-account impersonation
  roles, public principals, wildcards, domains, deleted principals, and direct
  users are rejected at organization scope.
- Only groups, service accounts, workload/workforce principals, and principal
  sets are valid organization members. Prefer groups for human access.
- Tag keys, values, and bindings set provider `deletion_policy = "PREVENT"` and
  Terraform `prevent_destroy = true`. IAM grants deliberately remain revocable.
- Stable map aliases are Terraform state addresses. Rename them only with an
  explicit `moved` block in the calling stack.

These checks are guardrails, not an IAM policy analyzer. A custom role can still
contain powerful permissions; custom roles require a separate permission review.

## Example

```hcl
module "organization_governance" {
  source = "../../modules/organization"

  organization_id = "123456789012"

  tag_keys = {
    environment = {
      short_name  = "environment"
      description = "Deployment environment"
      values = {
        production = { short_name = "production" }
        staging    = { short_name = "staging" }
      }
    }
  }

  tag_bindings = {
    production_project = {
      parent    = "//cloudresourcemanager.googleapis.com/projects/987654321098"
      tag_value = "environment/production"
    }
  }

  iam_grants = {
    security_reviewers = {
      role   = "roles/iam.securityReviewer"
      member = "group:cloud-security@example.com"
      condition = {
        title      = "time_bounded_review"
        expression = "request.time < timestamp('2027-01-01T00:00:00Z')"
      }
    }
  }
}
```

`tag_bindings.parent` must use a numeric full resource name:
`//cloudresourcemanager.googleapis.com/{organizations|folders|projects}/NUMBER`.
The `tag_value` field is a local `<key-alias>/<value-alias>` reference, not a
provider-generated tag ID.

## Inputs and limits

| Input | Contract | Module limit |
| --- | --- | ---: |
| `organization_id` | Numeric organization ID | one |
| `tag_keys` | Protected organization tag keys with nested values | 100 keys |
| nested `values` | Protected tag values | 100/key, 500 total |
| `tag_bindings` | Protected binding to a declared value | 500 |
| `iam_grants` | Additive role/member grant with optional condition | 500 |

The limits bound plan size and review blast radius; they are not statements of
Google Cloud quota. Short names intentionally use a conservative ASCII subset of
the broader Tags API character set.

## Outputs

`tag_keys`, `tag_values`, `tag_bindings`, and `iam_grants` preserve the caller's
stable aliases. Tag values use `<key-alias>/<value-alias>` keys.

## Lifecycle and operations

Enable the Resource Manager Tags API and grant the Terraform deployer explicit
tag-management and IAM permissions before planning. Review changes with saved
plans in CI. The module does not enable APIs or configure its own deployer.

Protected resources cannot be destroyed in a single apply. To retire a tag or
binding, first remove `prevent_destroy` and change its provider deletion policy in
a separately reviewed module release, apply that state change, and only then
remove the resource. IAM removal is a normal additive-member revocation and does
not require that sequence.

Mock-provider contract tests live in `tests/`; they validate graph shape and
guardrails but do not prove API permissions, quotas, CEL semantics, or behavior
against a live organization.

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
| <a name="input_iam_grants"></a> [iam\_grants](#input\_iam\_grants) | Additive organization IAM grants keyed by stable aliases. Only groups, service<br/>accounts, workforce/workload principals, and principal sets are accepted.<br/>Basic roles, administrator roles, service-account impersonation roles, public<br/>principals, domains, deleted principals, and direct users are deliberately<br/>rejected at organization scope. | <pre>map(object({<br/>    role   = string<br/>    member = string<br/>    condition = optional(object({<br/>      title       = string<br/>      description = optional(string, "")<br/>      expression  = string<br/>    }))<br/>  }))</pre> | `{}` | no |
| <a name="input_organization_id"></a> [organization\_id](#input\_organization\_id) | Numeric Google Cloud organization ID. | `string` | n/a | yes |
| <a name="input_tag_bindings"></a> [tag\_bindings](#input\_tag\_bindings) | Protected tag bindings keyed by stable aliases. parent must be a full numeric<br/>Cloud Resource Manager resource name. tag\_value references a value declared in<br/>tag\_keys using the form "<tag-key-alias>/<tag-value-alias>". | <pre>map(object({<br/>    parent    = string<br/>    tag_value = string<br/>  }))</pre> | `{}` | no |
| <a name="input_tag_keys"></a> [tag\_keys](#input\_tag\_keys) | Organization-scoped tag taxonomy keyed by stable Terraform aliases. Each key<br/>contains an optional map of tag values, also keyed by stable aliases. Aliases<br/>are state addresses and must not be renamed without an explicit moved block. | <pre>map(object({<br/>    short_name  = string<br/>    description = optional(string, "")<br/>    values = optional(map(object({<br/>      short_name  = string<br/>      description = optional(string, "")<br/>    })), {})<br/>  }))</pre> | `{}` | no |

## Outputs

| Name | Description |
| ---- | ----------- |
| <a name="output_iam_grants"></a> [iam\_grants](#output\_iam\_grants) | Additive organization IAM grants keyed by the caller-provided stable alias. |
| <a name="output_tag_bindings"></a> [tag\_bindings](#output\_tag\_bindings) | Protected tag bindings keyed by the caller-provided stable alias. |
| <a name="output_tag_keys"></a> [tag\_keys](#output\_tag\_keys) | Created tag keys keyed by the caller-provided stable alias. |
| <a name="output_tag_values"></a> [tag\_values](#output\_tag\_values) | Created tag values keyed by <tag-key-alias>/<tag-value-alias>. |
<!-- END_TF_DOCS -->
