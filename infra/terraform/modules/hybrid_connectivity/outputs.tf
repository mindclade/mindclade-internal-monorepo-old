# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

output "interconnect_self_links" { value = { for key, circuit in google_compute_interconnect.this : key => circuit.id } }
output "router_name" { value = google_compute_router.this.name }
output "activation_contract" {
  description = "Controls that protected attachment/BGP activation must supply with reviewed keys and peer coordinates."
  value = {
    administrative_state = "DISABLED"
    bgp_md5_required     = var.bgp_md5_authentication
    macsec_required      = var.macsec_enabled
    missing_live_inputs  = ["attachments", "peer_asn", "peer_ip_ranges", "secret-backed BGP MD5 and MACsec keys"]
  }
}
