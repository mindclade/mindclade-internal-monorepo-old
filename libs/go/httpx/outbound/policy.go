// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

package outbound

import (
	"crypto/tls"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

const (
	DefaultTimeout               = 30 * time.Second
	DefaultDialTimeout           = 10 * time.Second
	DefaultTLSHandshakeTimeout   = 10 * time.Second
	DefaultResponseHeaderTimeout = 20 * time.Second
	DefaultIdleConnTimeout       = 90 * time.Second
	DefaultMaxResponseBytes      = 16 << 20
	DefaultMaxRedirects          = 5
	DefaultMaxConnsPerHost       = 16
	DefaultUserAgent             = "mindclade-control-plane/1"
)

// Policy is immutable after client construction. Hostnames are exact canonical
// DNS names; broad wildcards are intentionally unsupported.
type Policy struct {
	AllowedHosts          []string
	AllowedPorts          []string
	AllowedMediaTypes     []string
	HTTPSOnly             bool
	AllowPrivateAddresses bool // intended only for controlled internal endpoints and tests
	AllowHTTPForTests     bool
	AllowGzip             bool
	Timeout               time.Duration
	DialTimeout           time.Duration
	TLSHandshakeTimeout   time.Duration
	ResponseHeaderTimeout time.Duration
	IdleConnTimeout       time.Duration
	MaxResponseBytes      int64
	MaxRedirects          int
	MaxConnsPerHost       int
	UserAgent             string
	TLSConfig             *tls.Config
	Proxy                 func(*http.Request) (*url.URL, error)
}

func (policy Policy) normalized() (Policy, error) {
	if policy.Timeout == 0 {
		policy.Timeout = DefaultTimeout
	}
	if policy.DialTimeout == 0 {
		policy.DialTimeout = DefaultDialTimeout
	}
	if policy.TLSHandshakeTimeout == 0 {
		policy.TLSHandshakeTimeout = DefaultTLSHandshakeTimeout
	}
	if policy.ResponseHeaderTimeout == 0 {
		policy.ResponseHeaderTimeout = DefaultResponseHeaderTimeout
	}
	if policy.IdleConnTimeout == 0 {
		policy.IdleConnTimeout = DefaultIdleConnTimeout
	}
	if policy.MaxResponseBytes == 0 {
		policy.MaxResponseBytes = DefaultMaxResponseBytes
	}
	if policy.MaxRedirects == 0 {
		policy.MaxRedirects = DefaultMaxRedirects
	}
	if policy.MaxConnsPerHost == 0 {
		policy.MaxConnsPerHost = DefaultMaxConnsPerHost
	}
	if strings.TrimSpace(policy.UserAgent) == "" {
		policy.UserAgent = DefaultUserAgent
	}
	if !policy.AllowHTTPForTests {
		policy.HTTPSOnly = true
	}

	if policy.Timeout < 0 || policy.DialTimeout < 0 || policy.TLSHandshakeTimeout < 0 ||
		policy.ResponseHeaderTimeout < 0 || policy.IdleConnTimeout < 0 || policy.MaxResponseBytes < 1 ||
		policy.MaxRedirects < 0 || policy.MaxConnsPerHost < 1 {
		return Policy{}, invalid(ErrInvalidPolicy, "invalid_outbound_limits")
	}
	if len(policy.AllowedHosts) == 0 {
		return Policy{}, invalid(ErrInvalidPolicy, "empty_allowed_hosts")
	}
	hosts := make([]string, 0, len(policy.AllowedHosts))
	seen := make(map[string]struct{}, len(policy.AllowedHosts))
	for _, raw := range policy.AllowedHosts {
		host := canonicalHost(raw)
		if host == "" {
			return Policy{}, invalid(ErrInvalidPolicy, "invalid_allowed_host")
		}
		if _, exists := seen[host]; exists {
			continue
		}
		seen[host] = struct{}{}
		hosts = append(hosts, host)
	}
	sort.Strings(hosts)
	policy.AllowedHosts = hosts

	ports := make([]string, 0, len(policy.AllowedPorts))
	for _, value := range policy.AllowedPorts {
		value = strings.TrimSpace(value)
		if !validPort(value) {
			return Policy{}, invalid(ErrInvalidPolicy, "invalid_allowed_port")
		}
		ports = append(ports, value)
	}
	sort.Strings(ports)
	policy.AllowedPorts = ports

	media := make([]string, 0, len(policy.AllowedMediaTypes))
	for _, value := range policy.AllowedMediaTypes {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" {
			return Policy{}, invalid(ErrInvalidPolicy, "invalid_allowed_media_type")
		}
		media = append(media, value)
	}
	sort.Strings(media)
	policy.AllowedMediaTypes = media
	return policy, nil
}
