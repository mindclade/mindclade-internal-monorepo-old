# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

# Matches the sibling modules and infrastructure-live's generated versions_override.tf, which
# is what actually binds under Terragrunt. `deletion_policy` on the DNS resources is 7.x-only,
# so a 6.x constraint here validates standalone and then fails at plan.
terraform {
  required_version = ">= 1.9.0, < 2.0.0"

  required_providers {
    google = {
      source  = "hashicorp/google"
      version = ">= 7.41.0, < 8.0.0"
    }
  }
}
