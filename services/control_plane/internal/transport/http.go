// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

package transport

import (
	"net"
	"net/http"

	"mindclade.internal/libs/go/faults"
	"mindclade.internal/libs/go/httpx"
	"mindclade.internal/libs/go/servicekit/production"
	"mindclade.internal/services/control_plane/internal/bootstrap"
)

// HTTP owns one pre-bound listener and the canonical httpx server lifecycle.
// Listener creation remains in the service factory so socket activation,
// network policy, and address ownership stay deployment-specific.
type HTTP struct {
	server   *httpx.Server
	listener net.Listener
	name     string
}

// NewHTTP constructs the only supported control-plane HTTP adapter.
func NewHTTP(name string, handler http.Handler, listener net.Listener, config httpx.ServerConfig) (*HTTP, error) {
	if listener == nil {
		return nil, faults.New(
			faults.CodeInvalidArgument,
			"HTTP listener is required",
			faults.WithReason("nil_http_listener"),
			faults.WithOperation("controlplane.transport.NewHTTP"),
			faults.WithRetryPolicy(faults.NoRetry()),
		)
	}
	server, err := httpx.NewServer(handler, config)
	if err != nil {
		return nil, err
	}
	return &HTTP{server: server, listener: listener, name: name}, nil
}

// Mechanism returns the canonical production capability/component pair.
func (adapter *HTTP) Mechanism() (bootstrap.Mechanism, error) {
	if adapter == nil || adapter.server == nil || adapter.listener == nil {
		return bootstrap.Mechanism{}, faults.New(
			faults.CodeFailedPrecondition,
			"HTTP adapter is not configured",
			faults.WithReason("http_adapter_not_configured"),
			faults.WithOperation("controlplane.transport.HTTP.Mechanism"),
			faults.WithRetryPolicy(faults.NoRetry()),
		)
	}
	return bootstrap.Mechanism{
		Capability: production.CapabilityHTTP,
		Component:  adapter.server.Component(adapter.name, adapter.listener),
	}, nil
}
