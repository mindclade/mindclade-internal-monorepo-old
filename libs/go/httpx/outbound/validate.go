// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package outbound

import (
	"net"
	"net/netip"
	"net/url"
	"strconv"
	"strings"

	"mindclade.internal/libs/go/faults"
)

func canonicalHost(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	value = strings.TrimSuffix(value, ".")
	if value == "" || strings.ContainsAny(value, "/\\@?#") {
		return ""
	}
	if host, _, err := net.SplitHostPort(value); err == nil {
		value = host
	}
	if strings.HasPrefix(value, "[") && strings.HasSuffix(value, "]") {
		value = strings.Trim(value, "[]")
	}
	return value
}

func validPort(value string) bool {
	if value == "" {
		return false
	}
	parsed, err := strconv.Atoi(value)
	return err == nil && parsed >= 1 && parsed <= 65535
}

func (policy Policy) validateURL(value *url.URL) error {
	if value == nil || value.User != nil || value.Fragment != "" || value.Host == "" {
		return reject(ErrURLRejected, "malformed_outbound_url", "httpx.outbound.Policy.ValidateURL", nil)
	}
	scheme := strings.ToLower(value.Scheme)
	plaintextPermitted := scheme == "http" && policy.AllowHTTPForTests
	if scheme != "https" && !plaintextPermitted {
		return reject(ErrURLRejected, "outbound_scheme_rejected", "httpx.outbound.Policy.ValidateURL", faults.Fields{"scheme": scheme})
	}
	if policy.HTTPSOnly && scheme != "https" {
		return reject(ErrURLRejected, "outbound_https_required", "httpx.outbound.Policy.ValidateURL", nil)
	}
	host := canonicalHost(value.Hostname())
	if host == "" {
		return reject(ErrHostNotAllowed, "invalid_outbound_host", "httpx.outbound.Policy.ValidateURL", nil)
	}
	if len(policy.AllowedHosts) > 0 && !contains(policy.AllowedHosts, host) {
		return reject(ErrHostNotAllowed, "outbound_host_not_allowed", "httpx.outbound.Policy.ValidateURL", faults.Fields{"host": host})
	}
	port := value.Port()
	if port == "" {
		if scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	if len(policy.AllowedPorts) > 0 && !contains(policy.AllowedPorts, port) {
		return reject(ErrHostNotAllowed, "outbound_port_not_allowed", "httpx.outbound.Policy.ValidateURL", faults.Fields{"port": port})
	}
	return nil
}

func allowedAddress(address netip.Addr, allowPrivate bool) bool {
	address = address.Unmap()
	if !address.IsValid() || address.IsUnspecified() || address.IsLoopback() || address.IsMulticast() || address.IsLinkLocalUnicast() || address.IsLinkLocalMulticast() {
		return allowPrivate && address.IsLoopback()
	}
	if address.IsPrivate() {
		return allowPrivate
	}
	// Reject benchmark, documentation, reserved, and IPv4-mapped special ranges.
	blocked := []netip.Prefix{
		netip.MustParsePrefix("0.0.0.0/8"), netip.MustParsePrefix("100.64.0.0/10"),
		netip.MustParsePrefix("192.0.0.0/24"), netip.MustParsePrefix("192.0.2.0/24"),
		netip.MustParsePrefix("198.18.0.0/15"), netip.MustParsePrefix("198.51.100.0/24"),
		netip.MustParsePrefix("203.0.113.0/24"), netip.MustParsePrefix("224.0.0.0/4"),
		netip.MustParsePrefix("240.0.0.0/4"), netip.MustParsePrefix("2001:db8::/32"),
		netip.MustParsePrefix("fc00::/7"), netip.MustParsePrefix("fe80::/10"),
	}
	for _, prefix := range blocked {
		if prefix.Contains(address) {
			return false
		}
	}
	return true
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
