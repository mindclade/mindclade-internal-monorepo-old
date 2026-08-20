# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

output "contact_ids" {
  description = "Essential Contacts resource ids keyed by <parent>:<email>."
  value       = { for key, contact in google_essential_contacts_contact.this : key => contact.id }
}

output "contact_count" {
  description = "Number of contacts managed by this module. A drop here between plans is the signal that a parent lost its routing."
  value       = length(google_essential_contacts_contact.this)
}

output "parents" {
  description = "Distinct parents that carry at least one contact."
  value       = sort(distinct([for c in local.contact_pairs : c.parent]))
}
