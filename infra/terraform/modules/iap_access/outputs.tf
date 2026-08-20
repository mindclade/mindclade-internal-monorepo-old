# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

output "bound_backend_services" {
  description = "Backend services this module bound, keyed by Kubernetes Service name."
  value       = var.backend_services
}

output "accessor_members" {
  description = <<-EOT
    The IAM members granted roles/iap.httpsResourceAccessor.

    Exported so a drift check can assert this set rather than re-deriving it — with a
    Google-managed OAuth client, an unexpected member here is the difference between an
    internal application and a public one.
  EOT
  value       = [for g in var.accessor_groups : "group:${g}"]
}
