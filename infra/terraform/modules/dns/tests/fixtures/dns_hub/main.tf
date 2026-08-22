# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

# Test-only projection of the retired dns_hub root. There is intentionally no backend,
# provider configuration, lock, credential bootstrap, or component registration here.
module "public_zones" {
  source = "../../.."

  project_id = "mc-fixture-dns"
  zones = {
    mindclade-ai = {
      dns_name   = "mindclade.ai."
      visibility = "public"
      dnssec     = true
      records = {
        caa = {
          name = "@"
          type = "CAA"
          rrdatas = [
            "0 issue \"pki.goog\"",
            "0 issue \"letsencrypt.org\"",
            "0 issuewild \";\"",
            "0 iodef \"mailto:security@mindclade.com\"",
          ]
        }
      }
    }
  }

  attached_networks  = []
  inbound_forwarding = { enabled = false }
  enable_logging     = true
  owner              = "platform"
}

output "zone_names" {
  value = module.public_zones.zone_names
}
