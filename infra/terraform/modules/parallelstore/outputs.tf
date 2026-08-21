# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

output "instances" {
  value = { for key, instance in google_parallelstore_instance.this : key => { name = instance.name, access_points = instance.access_points } }
}
output "gcs_import_contracts" {
  description = "Explicit post-create imports; execution requires a separately reviewed transfer workflow because the provider exposes no import resource."
  value       = { for key, instance in var.parallelstore : key => instance.gcs_import if instance.gcs_import != null }
}
