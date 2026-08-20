# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

output "folder_ids" {
  description = "Folder resource ids as folders/<numeric id>, keyed by the short name. This is the form every downstream parent field expects."
  value       = { for name, folder in google_folder.this : name => folder.id }
}

output "folder_numbers" {
  description = "Bare numeric folder ids, keyed by short name, for the APIs that reject the folders/ prefix."
  value       = { for name, folder in google_folder.this : name => trimprefix(folder.id, "folders/") }
}

output "folder_names" {
  description = "Display names as created, keyed by short name."
  value       = { for name, folder in google_folder.this : name => folder.display_name }
}

output "budget_ids" {
  description = "Billing budget resource ids keyed by folder short name."
  value       = { for name, budget in google_billing_budget.folder : name => budget.id }
}
