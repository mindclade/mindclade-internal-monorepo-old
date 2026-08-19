# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

# Matches modules/dns exactly. `deletion_policy` on the zone resources is
# provider 7.x-only, so a 6.x constraint here would validate standalone and then
# fail at plan with an unrecognised-argument error that names the module rather
# than this file.
terraform {
  required_version = ">= 1.9.0, < 2.0.0"

  required_providers {
    google = {
      source  = "hashicorp/google"
      version = ">= 7.41.0, < 8.0.0"
    }
  }
}

provider "google" {
  project = var.project_id

  # Null falls back to the caller's own credentials, which is the only option
  # during the very first apply -- the service account does not exist yet. See
  # the variable's documentation for why leaving it null afterwards is a
  # standing risk rather than a neutral default.
  impersonate_service_account = var.impersonate_service_account
}
