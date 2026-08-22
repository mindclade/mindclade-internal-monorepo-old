# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

data "google_access_context_manager_access_policy" "this" {
  parent = "organizations/${var.org_id}"
}

resource "google_access_context_manager_service_perimeter" "this" {
  parent                    = data.google_access_context_manager_access_policy.this.name
  name                      = "${data.google_access_context_manager_access_policy.this.name}/servicePerimeters/${var.perimeter.name}"
  title                     = var.perimeter.title
  perimeter_type            = "PERIMETER_TYPE_REGULAR"
  use_explicit_dry_run_spec = true
  deletion_policy           = "PREVENT"

  spec {
    resources           = var.perimeter.resources
    restricted_services = var.perimeter.restricted_services

    vpc_accessible_services {
      enable_restriction = var.perimeter.vpc_accessible_services.enable_restriction
      allowed_services   = var.perimeter.vpc_accessible_services.allowed_services
    }

    dynamic "ingress_policies" {
      for_each = var.ingress_policies
      content {
        title = ingress_policies.value.title
        ingress_from {
          identities    = ingress_policies.value.from.identities
          identity_type = ingress_policies.value.from.identity_type
          dynamic "sources" {
            for_each = ingress_policies.value.from.source_access_levels
            content { access_level = sources.value }
          }
        }
        ingress_to {
          resources = ingress_policies.value.to.resources
          dynamic "operations" {
            for_each = ingress_policies.value.to.operations
            content {
              service_name = operations.key
              dynamic "method_selectors" {
                for_each = operations.value.methods
                content { method = method_selectors.value }
              }
            }
          }
        }
      }
    }
  }

  lifecycle {
    prevent_destroy = true
    precondition {
      condition     = data.google_access_context_manager_access_policy.this.title == var.policy_name
      error_message = "The organization's sole Access Context Manager policy title does not match policy_name."
    }
    precondition {
      condition     = length(var.egress_policies) == 0 && length(var.access_levels) == 0
      error_message = "The initial perimeter contract forbids egress policies and IP-based access levels."
    }
  }
}
