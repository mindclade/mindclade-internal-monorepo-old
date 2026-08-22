# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

mock_provider "google" {}

run "exact_provider_allowlist_is_fail_closed" {
  command = plan

  variables {
    proxies = {
      production = {
        project_id                = "mindclade-production-platform"
        region                    = "us-central1"
        name                      = "provider-egress"
        scope                     = "production"
        address                   = "10.20.1.10"
        network                   = "projects/mindclade-production-platform/global/networks/production"
        subnetwork                = "projects/mindclade-production-platform/regions/us-central1/subnetworks/provider-egress"
        gateway_certificate_url   = "projects/mindclade-production-platform/locations/us-central1/certificates/provider-egress"
        tls_inspection_policy_url = "projects/mindclade-production-platform/locations/us-central1/tlsInspectionPolicies/provider-egress"
        allowed_hosts             = ["api.openai.com", "generativelanguage.googleapis.com"]
      }
    }
  }

  assert {
    condition = (
      google_network_security_gateway_security_policy.proxy["production"].deletion_policy == "PREVENT" &&
      google_network_security_gateway_security_policy_rule.allow_provider_hosts["production"].basic_profile == "ALLOW" &&
      google_network_security_gateway_security_policy_rule.allow_provider_hosts["production"].tls_inspection_enabled &&
      google_network_security_gateway_security_policy_rule.deny_all["production"].basic_profile == "DENY" &&
      google_network_security_gateway_security_policy_rule.deny_all["production"].priority == 2147483646
    )
    error_message = "The proxy policy must retain TLS inspection, deletion protection, and an explicit terminal deny."
  }

  assert {
    condition = (
      google_network_services_gateway.proxy["production"].type == "SECURE_WEB_GATEWAY" &&
      google_network_services_gateway.proxy["production"].routing_mode == "EXPLICIT_ROUTING_MODE" &&
      length(google_network_services_gateway.proxy["production"].ports) == 1 &&
      google_network_services_gateway.proxy["production"].ports[0] == 443 &&
      google_network_services_gateway.proxy["production"].envoy_headers == "NONE" &&
      output.https_proxy_urls["production"] == "https://10.20.1.10:443"
    )
    error_message = "Provider egress must use the explicit HTTPS proxy without injected Envoy headers."
  }

  assert {
    condition = (
      google_network_security_gateway_security_policy_rule.allow_provider_hosts["production"].session_matcher ==
      "host() == 'api.openai.com' || host() == 'generativelanguage.googleapis.com'" &&
      google_network_security_gateway_security_policy_rule.allow_provider_hosts["production"].application_matcher ==
      "request.host() == 'api.openai.com' || request.host() == 'generativelanguage.googleapis.com'"
    )
    error_message = "Only the normalized, exact provider hostname allowlist may reach the allow rule."
  }
}

run "wildcard_provider_host_is_rejected" {
  command = plan

  variables {
    proxies = {
      production = {
        project_id                = "mindclade-production-platform"
        region                    = "us-central1"
        name                      = "provider-egress"
        scope                     = "production"
        address                   = "10.20.1.10"
        network                   = "projects/mindclade-production-platform/global/networks/production"
        subnetwork                = "projects/mindclade-production-platform/regions/us-central1/subnetworks/provider-egress"
        gateway_certificate_url   = "projects/mindclade-production-platform/locations/us-central1/certificates/provider-egress"
        tls_inspection_policy_url = "projects/mindclade-production-platform/locations/us-central1/tlsInspectionPolicies/provider-egress"
        allowed_hosts             = ["*.openai.com"]
      }
    }
  }

  expect_failures = [var.proxies]
}

run "cross_region_subnetwork_is_rejected" {
  command = plan

  variables {
    proxies = {
      production = {
        project_id                = "mindclade-production-platform"
        region                    = "us-central1"
        name                      = "provider-egress"
        scope                     = "production"
        address                   = "10.20.1.10"
        network                   = "projects/mindclade-production-platform/global/networks/production"
        subnetwork                = "projects/mindclade-production-platform/regions/us-east1/subnetworks/provider-egress"
        gateway_certificate_url   = "projects/mindclade-production-platform/locations/us-central1/certificates/provider-egress"
        tls_inspection_policy_url = "projects/mindclade-production-platform/locations/us-central1/tlsInspectionPolicies/provider-egress"
        allowed_hosts             = ["api.openai.com"]
      }
    }
  }

  expect_failures = [var.proxies]
}
