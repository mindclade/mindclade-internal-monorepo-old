# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

# Reserved internal IPv4 addresses.
#
# Deliberately its own module rather than a field on the network module. An address is
# reserved once and then referenced by name from outside Terraform — a GKE Gateway names it in
# `spec.addresses[].value` — so its lifecycle is tied to the thing that consumes it, not to
# the subnet it comes from. Putting it in the network module would mean a subnet change and an
# address change share a state file and a blast radius.

resource "google_compute_address" "this" {
  for_each = var.addresses

  project     = each.value.project_id
  name        = each.value.name
  description = each.value.description
  region      = each.value.region

  address_type    = "INTERNAL"
  purpose         = each.value.purpose
  subnetwork      = each.value.subnetwork
  deletion_policy = "PREVENT"

  # null lets GCP allocate. Pin it once a DNS record points here: an unpinned address can come
  # back different after a destroy and recreate, and the private-zone A records would then
  # resolve to an address nothing is listening on.
  address = each.value.address

  lifecycle {
    # An address that changes is worse than one that cannot be deleted. Every private DNS
    # record in the estate points at these, and none of them would fail — they would resolve
    # to nothing, which presents as an application outage rather than a DNS one.
    prevent_destroy = true
  }
}
