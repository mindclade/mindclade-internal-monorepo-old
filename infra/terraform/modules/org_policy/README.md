# `org_policy`

Organization Policy v2, at an organization or folder, with per-folder overrides.

v2 rather than the v1 `google_organization_policy`, because v1 cannot express a folder-level
override without replacing the whole policy — and that replacement is exactly the operation
that loses an org-level rule nobody remembered was there.

## Things that bite

- **The policy name is the identity.** Moving a constraint between `boolean_policies` and
  `list_policies` is a destroy-and-create, and the window in between is a window with no
  enforcement. A constraint declared in both maps fails a precondition rather than colliding
  at apply.
- **`false` is not the same as omitted.** An omitted constraint inherits; a `false` one writes
  an explicit override that cancels an inherited enforcement.
- **`allowed_values` and `denied_values` are exclusive.** Google evaluates an allow list as
  "nothing else", so setting both produces a rule whose effect depends on evaluation order
  rather than on what was written. Both empty is rejected too — it enforces nothing while
  reading as a rule.
- **`deny_all = false` resets; `allow_all = true` does not.** A reset restores the inherited
  default and lets a later org-level tightening reach the folder. `allow_all` inherits
  nothing, so the folder silently escapes every future change.
- **Every override needs a reason of 20+ characters**, surfaced in the `override_reasons`
  output so a relaxation appears in state and in a plan diff rather than only in the caller.

`inherit_from_parent` is set only when the parent is a folder — Google rejects it at the
organization level, with an error naming the field but not the reason.

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
| <a name="input_boolean_policies"></a> [boolean\_policies](#input\_boolean\_policies) | Boolean constraints keyed by constraint name, without the constraints/ prefix. true<br/>enforces the constraint; false explicitly does not, which is different from omitting it —<br/>an omitted constraint inherits, a false one overrides an inherited enforcement. | `map(bool)` | `{}` | no |
| <a name="input_folder_overrides"></a> [folder\_overrides](#input\_folder\_overrides) | Per-folder relaxations, keyed by folders/<id> then by constraint name. Every override<br/>carries a reason, because a relaxation whose justification lives in a commit message is<br/>one nobody can review a year later.<br/><br/>deny\_all = false RESETS to the inherited default rather than permitting everything.<br/>allow\_all = true permits everything and inherits nothing, so a later org-level tightening<br/>silently does not reach the folder — prefer the reset. | <pre>map(map(object({<br/>    allow_all = optional(bool)<br/>    deny_all  = optional(bool)<br/>    enforce   = optional(bool)<br/>    reason    = string<br/>  })))</pre> | `{}` | no |
| <a name="input_list_policies"></a> [list\_policies](#input\_list\_policies) | List constraints keyed by constraint name. Exactly one of allowed\_values or denied\_values<br/>may be non-empty: Google evaluates an allow list as "nothing else", so supplying both is<br/>a rule whose effect depends on evaluation order rather than on what was written. | <pre>map(object({<br/>    allowed_values = optional(list(string), [])<br/>    denied_values  = optional(list(string), [])<br/>  }))</pre> | `{}` | no |
| <a name="input_parent"></a> [parent](#input\_parent) | Resource the organization policies attach to, as organizations/<id> or folders/<id> | `string` | n/a | yes |

## Outputs

| Name | Description |
| ---- | ----------- |
| <a name="output_boolean_policy_ids"></a> [boolean\_policy\_ids](#output\_boolean\_policy\_ids) | Boolean org policy resource ids keyed by constraint name. |
| <a name="output_enforced_constraints"></a> [enforced\_constraints](#output\_enforced\_constraints) | Constraint names actively enforced at the parent. The list a reviewer checks against the last approved set. |
| <a name="output_folder_override_ids"></a> [folder\_override\_ids](#output\_folder\_override\_ids) | Folder override policy ids keyed by <folder>:<constraint>. |
| <a name="output_list_policy_ids"></a> [list\_policy\_ids](#output\_list\_policy\_ids) | List org policy resource ids keyed by constraint name. |
| <a name="output_override_reasons"></a> [override\_reasons](#output\_override\_reasons) | Why each folder is exempt, keyed by <folder>:<constraint>. Surfaced as an output so a relaxation is visible in state, not only in the caller. |
<!-- END_TF_DOCS -->
