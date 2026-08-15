// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package grpcx

import (
	"context"
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"google.golang.org/grpc"

	"mindclade.internal/libs/go/faults"
	"mindclade.internal/libs/go/servicekit"
)

type Server struct {
	server              *grpc.Server
	gracefulStopTimeout time.Duration
	serving             atomic.Bool
	started             atomic.Bool
	mu                  sync.RWMutex
	terminalErr         error
}

func NewServer(config ServerConfig, additional ...grpc.ServerOption) (*Server, error) {
	normalized := config.normalized()
	options, err := ServerOptions(normalized)
	if err != nil {
		return nil, err
	}
	for _, option := range additional {
		if option != nil {
			options = append(options, option)
		}
	}
	return &Server{server: grpc.NewServer(options...), gracefulStopTimeout: normalized.GracefulStopTimeout}, nil
}
func (server *Server) GRPCServer() *grpc.Server {
	if server == nil {
		return nil
	}
	return server.server
}
func (server *Server) Serve(listener net.Listener) error {
	if server == nil || server.server == nil {
		return faults.New(faults.CodeFailedPrecondition, "gRPC server is not initialized", faults.WithReason("nil_grpc_server"))
	}
	if nilInterface(listener) {
		return faults.Wrap(ErrNilListener, faults.CodeInvalidArgument, "gRPC listener is required", faults.WithReason("nil_grpc_listener"))
	}
	if !server.started.CompareAndSwap(false, true) {
		return faults.New(faults.CodeFailedPrecondition, "gRPC server has already been started", faults.WithReason("grpc_server_already_started"))
	}
	server.serving.Store(true)
	err := server.server.Serve(listener)
	server.serving.Store(false)
	if errors.Is(err, grpc.ErrServerStopped) {
		err = nil
	}
	server.mu.Lock()
	server.terminalErr = err
	server.mu.Unlock()
	if err != nil {
		return faults.Wrap(err, faults.CodeUnavailable, "gRPC server stopped unexpectedly", faults.WithReason("grpc_serve_failed"), faults.WithOperation("grpcx.Server.Serve"), faults.WithRetryPolicy(faults.BackoffRetry(0)))
	}
	return nil
}
func (server *Server) Shutdown(ctx context.Context) error {
	if server == nil || server.server == nil {
		return nil
	}
	if ctx == nil {
		return faults.New(faults.CodeInvalidArgument, "shutdown context is required", faults.WithReason("nil_context"))
	}
	if !server.serving.Load() {
		return nil
	}
	done := make(chan struct{})
	go func() { server.server.GracefulStop(); close(done) }()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		server.server.Stop()
		<-done
		code := faults.CodeOf(ctx.Err())
		reason := "grpc_shutdown_interrupted"
		message := "gRPC server graceful shutdown was interrupted"
		if code == faults.CodeDeadlineExceeded {
			reason = "grpc_shutdown_timeout"
			message = "gRPC server graceful shutdown timed out"
		} else if code == faults.CodeCanceled {
			reason = "grpc_shutdown_canceled"
			message = "gRPC server graceful shutdown was canceled"
		} else if code == faults.CodeUnknown {
			code = faults.CodeInternal
		}
		return faults.Wrap(ctx.Err(), code, message, faults.WithReason(reason), faults.WithOperation("grpcx.Server.Shutdown"))
	}
}
func (server *Server) Stop() {
	if server != nil && server.server != nil {
		server.server.Stop()
	}
}
func (server *Server) Serving() bool { return server != nil && server.serving.Load() }
func (server *Server) Err() error {
	if server == nil {
		return ErrNotServing
	}
	server.mu.RLock()
	defer server.mu.RUnlock()
	return server.terminalErr
}
func (server *Server) Component(name string, listener net.Listener) servicekit.Component {
	return servicekit.Component{
		Name: name,
		Start: func(context.Context) error {
			if server == nil || server.server == nil {
				return faults.New(faults.CodeFailedPrecondition, "gRPC server is not initialized", faults.WithReason("nil_grpc_server"))
			}
			if nilInterface(listener) {
				return faults.Wrap(ErrNilListener, faults.CodeInvalidArgument, "gRPC listener is required", faults.WithReason("nil_grpc_listener"))
			}
			return nil
		},
		Run: func(context.Context) error { return server.Serve(listener) },
		Stop: func(ctx context.Context) error {
			if server == nil {
				return nil
			}
			shutdownCtx := ctx
			cancel := func() {}
			if _, ok := ctx.Deadline(); !ok {
				shutdownCtx, cancel = context.WithTimeout(ctx, server.gracefulStopTimeout)
			}
			defer cancel()
			return server.Shutdown(shutdownCtx)
		},
		Liveness: func(context.Context) error { return server.Err() },
		Readiness: func(context.Context) error {
			if !server.Serving() {
				return faults.Wrap(ErrNotServing, faults.CodeUnavailable, "gRPC server is not ready", faults.WithReason("grpc_not_serving"), faults.WithRetryPolicy(faults.ImmediateRetry(0)))
			}
			return nil
		},
	}
}
