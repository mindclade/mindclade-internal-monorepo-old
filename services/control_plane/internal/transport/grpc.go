// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package transport

import (
	"net"

	"google.golang.org/grpc"

	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/grpcx"
	"go.mindclade.dev/libs/go/servicekit/production"
	"go.mindclade.dev/services/control_plane/internal/bootstrap"
)

// GRPCRegistrar installs generated services, health, reflection, or other
// service-owned registrations on the canonical server before it starts.
type GRPCRegistrar func(*grpc.Server) error

// GRPC owns one pre-bound listener and the canonical grpcx server lifecycle.
type GRPC struct {
	server   *grpcx.Server
	listener net.Listener
	name     string
}

// NewGRPC constructs the only supported control-plane gRPC adapter.
func NewGRPC(name string, listener net.Listener, config grpcx.ServerConfig, registrars ...GRPCRegistrar) (*GRPC, error) {
	if listener == nil {
		return nil, faults.New(
			faults.CodeInvalidArgument,
			"gRPC listener is required",
			faults.WithReason("nil_grpc_listener"),
			faults.WithOperation("controlplane.transport.NewGRPC"),
			faults.WithRetryPolicy(faults.NoRetry()),
		)
	}
	server, err := grpcx.NewServer(config)
	if err != nil {
		return nil, err
	}
	for _, register := range registrars {
		if register == nil {
			continue
		}
		if err := register(server.GRPCServer()); err != nil {
			return nil, err
		}
	}
	return &GRPC{server: server, listener: listener, name: name}, nil
}

// Mechanism returns the canonical production capability/component pair.
func (adapter *GRPC) Mechanism() (bootstrap.Mechanism, error) {
	if adapter == nil || adapter.server == nil || adapter.listener == nil {
		return bootstrap.Mechanism{}, faults.New(
			faults.CodeFailedPrecondition,
			"gRPC adapter is not configured",
			faults.WithReason("grpc_adapter_not_configured"),
			faults.WithOperation("controlplane.transport.GRPC.Mechanism"),
			faults.WithRetryPolicy(faults.NoRetry()),
		)
	}
	return bootstrap.Mechanism{
		Capability: production.CapabilityGRPC,
		Component:  adapter.server.Component(adapter.name, adapter.listener),
	}, nil
}
