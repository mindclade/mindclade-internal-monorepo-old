// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package httpx

import (
	"crypto/tls"
	"net"
	"net/http"
	"time"

	"mindclade.internal/libs/go/faults"
)

const (
	DefaultClientTimeout         = 30 * time.Second
	DefaultDialTimeout           = 10 * time.Second
	DefaultDialKeepAlive         = 30 * time.Second
	DefaultTLSHandshakeTimeout   = 10 * time.Second
	DefaultResponseHeaderTimeout = 30 * time.Second
	DefaultExpectContinueTimeout = time.Second
	DefaultMaxIdleConns          = 100
	DefaultMaxIdleConnsPerHost   = 16
)

// TransportConfig configures an isolated http.Transport. It never mutates
// http.DefaultTransport.
type TransportConfig struct {
	DialTimeout           time.Duration
	DialKeepAlive         time.Duration
	TLSHandshakeTimeout   time.Duration
	ResponseHeaderTimeout time.Duration
	ExpectContinueTimeout time.Duration
	IdleConnTimeout       time.Duration
	MaxIdleConns          int
	MaxIdleConnsPerHost   int
	MaxConnsPerHost       int
	DisableCompression    bool
	TLSConfig             *tls.Config
}

func (config TransportConfig) normalized() TransportConfig {
	if config.DialTimeout == 0 {
		config.DialTimeout = DefaultDialTimeout
	}
	if config.DialKeepAlive == 0 {
		config.DialKeepAlive = DefaultDialKeepAlive
	}
	if config.TLSHandshakeTimeout == 0 {
		config.TLSHandshakeTimeout = DefaultTLSHandshakeTimeout
	}
	if config.ResponseHeaderTimeout == 0 {
		config.ResponseHeaderTimeout = DefaultResponseHeaderTimeout
	}
	if config.ExpectContinueTimeout == 0 {
		config.ExpectContinueTimeout = DefaultExpectContinueTimeout
	}
	if config.IdleConnTimeout == 0 {
		config.IdleConnTimeout = DefaultIdleTimeout
	}
	if config.MaxIdleConns == 0 {
		config.MaxIdleConns = DefaultMaxIdleConns
	}
	if config.MaxIdleConnsPerHost == 0 {
		config.MaxIdleConnsPerHost = DefaultMaxIdleConnsPerHost
	}
	return config
}

func NewTransport(config TransportConfig) (*http.Transport, error) {
	config = config.normalized()
	if config.DialTimeout < 0 || config.DialKeepAlive < 0 || config.TLSHandshakeTimeout < 0 ||
		config.ResponseHeaderTimeout < 0 || config.ExpectContinueTimeout < 0 || config.IdleConnTimeout < 0 ||
		config.MaxIdleConns < 0 || config.MaxIdleConnsPerHost < 0 || config.MaxConnsPerHost < 0 {
		return nil, faults.Wrap(ErrInvalidConfig, faults.CodeInvalidArgument, "invalid HTTP transport configuration", faults.WithReason("invalid_http_transport_config"))
	}
	dialer := &net.Dialer{Timeout: config.DialTimeout, KeepAlive: config.DialKeepAlive}
	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           dialer.DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          config.MaxIdleConns,
		MaxIdleConnsPerHost:   config.MaxIdleConnsPerHost,
		MaxConnsPerHost:       config.MaxConnsPerHost,
		IdleConnTimeout:       config.IdleConnTimeout,
		TLSHandshakeTimeout:   config.TLSHandshakeTimeout,
		ResponseHeaderTimeout: config.ResponseHeaderTimeout,
		ExpectContinueTimeout: config.ExpectContinueTimeout,
		DisableCompression:    config.DisableCompression,
	}
	if config.TLSConfig != nil {
		transport.TLSClientConfig = config.TLSConfig.Clone()
	}
	return transport, nil
}

type ClientConfig struct {
	Timeout   time.Duration
	Transport TransportConfig
}

func NewClient(config ClientConfig) (*http.Client, error) {
	if config.Timeout == 0 {
		config.Timeout = DefaultClientTimeout
	}
	if config.Timeout < 0 {
		return nil, faults.Wrap(ErrInvalidConfig, faults.CodeInvalidArgument, "invalid HTTP client configuration", faults.WithReason("invalid_http_client_config"))
	}
	transport, err := NewTransport(config.Transport)
	if err != nil {
		return nil, err
	}
	return &http.Client{Transport: RequestMetadataTransport{Base: transport}, Timeout: config.Timeout}, nil
}

// RoundTripperFunc adapts a function to http.RoundTripper.
type RoundTripperFunc func(*http.Request) (*http.Response, error)

func (function RoundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}
