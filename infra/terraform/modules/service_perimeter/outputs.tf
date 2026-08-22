# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

output "perimeter_name" { value = google_access_context_manager_service_perimeter.this.name }
output "dry_run" { value = google_access_context_manager_service_perimeter.this.use_explicit_dry_run_spec }
