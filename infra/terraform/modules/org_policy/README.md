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
