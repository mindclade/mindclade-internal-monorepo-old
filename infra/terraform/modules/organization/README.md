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
