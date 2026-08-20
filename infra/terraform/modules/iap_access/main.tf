# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#
# Who may complete an IAP sign-in.
#
# ===========================================================================================
# This binding is load-bearing, and it did not used to be
# ===========================================================================================
# The design assumed a CUSTOM OAuth client with an INTERNAL audience, so that only members of
# the organization could reach the consent screen at all. That is no longer possible for
# anyone: Google deprecated the IAP OAuth Admin APIs on 22 Jan 2025 and permanently shut them
# down on 19 March 2026. IAP now uses a Google-managed client.
#
# The practical consequence is that ANY Google account can reach the consent screen. What
# stops an outsider from getting further is exactly this binding — nothing else. It was
# defence in depth under the original design; under the Google-managed client it is the
# control.
#
# So the two rules below are not stylistic:
#
#   NEVER allUsers or allAuthenticatedUsers.  `allAuthenticatedUsers` reads like "people who
#     signed in" and actually means "anyone with a Google account, anywhere". On a
#     Google-managed client that is the entire internet, and the resulting access is silent —
#     the sign-in simply succeeds.
#
#   GROUPS, not individuals.  A binding naming a person outlives their employment; a group
#     membership is removed by the same offboarding that removes everything else. This is the
#     opposite of the break-glass convention elsewhere in the estate, and deliberately so:
#     break-glass needs an audit trail that names a human, while routine access needs a
#     revocation path that cannot be forgotten.
#
# Revocation runs through here too. Removing someone from the group stops IAP asserting for
# them, and the browser plane's five-minute session cache then expires — which is what bounds
# revocation latency at five minutes rather than at a session lifetime.

locals {
  accessor_members = merge([
    for backend_key, backend_name in var.backend_services : {
      for group in var.accessor_groups : "${backend_key}/${group}" => {
        backend_name = backend_name
        group        = group
      }
    }
  ]...)
}

resource "google_iap_web_backend_service_iam_member" "accessor" {
  for_each = local.accessor_members

  project             = var.project_id
  web_backend_service = each.value.backend_name
  role                = "roles/iap.httpsResourceAccessor"
  member              = "group:${each.value.group}"
}
