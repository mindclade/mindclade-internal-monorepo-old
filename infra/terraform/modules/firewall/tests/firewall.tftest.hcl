# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

mock_provider "google" {}

run "logged_default_deny" {
  command = plan
  variables {
    firewalls = {
      production = {
        project_id = "mindclade-production-net"
        network    = "projects/mindclade-production-net/global/networks/production-vpc"
        rules = {
          deny-egress-default = {
            direction          = "EGRESS"
            priority           = 65000
            action             = "deny"
            destination_ranges = ["0.0.0.0/0"]
            deny               = [{ protocol = "all" }]
          }
        }
      }
    }
  }
  assert {
    condition = (
      google_compute_firewall.rule["production/deny-egress-default"].direction == "EGRESS" &&
      google_compute_firewall.rule["production/deny-egress-default"].priority == 65000 &&
      google_compute_firewall.rule["production/deny-egress-default"].log_config[0].metadata == "INCLUDE_ALL_METADATA"
    )
    error_message = "Default egress deny must remain last and metadata-rich."
  }
}

run "reject_rule_with_allow_and_deny" {
  command = plan
  variables {
    firewalls = {
      invalid = {
        project_id = "mindclade-development-net"
        network    = "projects/mindclade-development-net/global/networks/development-vpc"
        rules = {
          invalid = {
            direction = "EGRESS"
            priority  = 1000
            action    = "allow"
            allow     = [{ protocol = "tcp", ports = ["443"] }]
            deny      = [{ protocol = "all" }]
          }
        }
      }
    }
  }
  expect_failures = [var.firewalls]
}
