# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

locals {
  proxies = {
    for key, proxy in var.proxies : key => merge(proxy, {
      allowed_hosts = sort(distinct(proxy.allowed_hosts))
    })
  }

  # One allow rule is deliberate. Google Cloud rejects parallel rule creation for a policy;
  # folding the bounded allowlist into one CEL expression avoids an eventually-consistent
  # for_each race and makes the deny rule's dependency exact.
  session_matchers = {
    for key, proxy in local.proxies : key => join(" || ", [
      for host in proxy.allowed_hosts : format("host() == '%s'", host)
    ])
  }
  application_matchers = {
    for key, proxy in local.proxies : key => join(" || ", [
      for host in proxy.allowed_hosts : format("request.host() == '%s'", host)
    ])
  }
}

resource "google_network_security_gateway_security_policy" "proxy" {
  for_each = local.proxies

  project               = each.value.project_id
  location              = each.value.region
  name                  = "${each.value.name}-policy"
  description           = "Fail-closed outbound provider policy for ${each.key}."
  tls_inspection_policy = each.value.tls_inspection_policy_url
  deletion_policy       = "PREVENT"

  lifecycle {
    prevent_destroy = true
  }
}

resource "google_network_security_gateway_security_policy_rule" "allow_provider_hosts" {
  for_each = local.proxies

  project                 = each.value.project_id
  location                = each.value.region
  gateway_security_policy = google_network_security_gateway_security_policy.proxy[each.key].name
  name                    = "allow-provider-hosts"
  description             = "Allow only reviewed provider hostnames after TLS inspection."
  enabled                 = true
  priority                = 100
  session_matcher         = local.session_matchers[each.key]
  application_matcher     = local.application_matchers[each.key]
  tls_inspection_enabled  = true
  basic_profile           = "ALLOW"
  deletion_policy         = "PREVENT"

  lifecycle {
    prevent_destroy = true
  }
}

resource "google_network_security_gateway_security_policy_rule" "deny_all" {
  for_each = local.proxies

  project                 = each.value.project_id
  location                = each.value.region
  gateway_security_policy = google_network_security_gateway_security_policy.proxy[each.key].name
  name                    = "deny-all"
  description             = "Explicit terminal deny for unmatched outbound traffic."
  enabled                 = true
  priority                = 2147483646
  session_matcher         = "true"
  basic_profile           = "DENY"
  deletion_policy         = "PREVENT"

  depends_on = [google_network_security_gateway_security_policy_rule.allow_provider_hosts]

  lifecycle {
    prevent_destroy = true
  }
}

resource "google_network_services_gateway" "proxy" {
  for_each = local.proxies

  project                 = each.value.project_id
  location                = each.value.region
  name                    = each.value.name
  description             = "Explicit Secure Web Proxy for governed provider egress."
  type                    = "SECURE_WEB_GATEWAY"
  routing_mode            = "EXPLICIT_ROUTING_MODE"
  scope                   = each.value.scope
  addresses               = [each.value.address]
  ports                   = [443]
  certificate_urls        = [each.value.gateway_certificate_url]
  gateway_security_policy = google_network_security_gateway_security_policy.proxy[each.key].id
  network                 = each.value.network
  subnetwork              = each.value.subnetwork
  envoy_headers           = "NONE"
  deletion_policy         = "PREVENT"

  depends_on = [google_network_security_gateway_security_policy_rule.deny_all]

  lifecycle {
    prevent_destroy = true
  }
}
