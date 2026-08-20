# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#
# Folder- and project-scoped Essential Contacts.
#
# An org-level contact receives everything for everywhere. A GPU quota warning in development
# and a suspension notice on production arrive in the same inbox with the same weight, and
# within a month the filter rule somebody wrote to cope means neither is read. Scoping the
# routing is what keeps the important message legible.
#
# Contacts do not inherit downward past a more specific subscription for the same category,
# so a folder contact genuinely replaces the org one for that folder rather than doubling it.

locals {
  # Flattened to one resource per (parent, email). The map key has to be stable across a
  # reorder of the list, so it is built from the two values that identify the contact rather
  # than from a list index — an index key would move every contact below an insertion.
  contact_pairs = merge([
    for parent, list in var.contacts : {
      for c in list : "${parent}:${c.email}" => {
        parent        = parent
        email         = c.email
        subscriptions = c.subscriptions
        language_tag  = c.language_tag
      }
    }
  ]...)
}

resource "google_essential_contacts_contact" "this" {
  for_each = local.contact_pairs

  parent                              = each.value.parent
  email                               = each.value.email
  language_tag                        = each.value.language_tag
  notification_category_subscriptions = each.value.subscriptions
}
