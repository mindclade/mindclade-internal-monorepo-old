# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

output "rule_ids" {
  description = "Firewall rule IDs keyed by owner and rule name."
  value       = { for key, rule in google_compute_firewall.rule : key => rule.id }
}

output "rule_names" {
  description = "Firewall rule names keyed by owner and rule name."
  value       = { for key, rule in google_compute_firewall.rule : key => rule.name }
}
