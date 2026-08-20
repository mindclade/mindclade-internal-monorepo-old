# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

output "boolean_policy_ids" {
  description = "Boolean org policy resource ids keyed by constraint name."
  value       = { for name, p in google_org_policy_policy.boolean : name => p.id }
}

output "list_policy_ids" {
  description = "List org policy resource ids keyed by constraint name."
  value       = { for name, p in google_org_policy_policy.list : name => p.id }
}

output "folder_override_ids" {
  description = "Folder override policy ids keyed by <folder>:<constraint>."
  value       = { for key, p in google_org_policy_policy.folder_override : key => p.id }
}

output "enforced_constraints" {
  description = "Constraint names actively enforced at the parent. The list a reviewer checks against the last approved set."
  value = sort(concat(
    [for name, enforced in var.boolean_policies : name if enforced],
    keys(var.list_policies),
  ))
}

output "override_reasons" {
  description = "Why each folder is exempt, keyed by <folder>:<constraint>. Surfaced as an output so a relaxation is visible in state, not only in the caller."
  value       = { for key, o in local.overrides : key => o.reason }
}
