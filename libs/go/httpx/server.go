// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package httpx

import (
	"context"
	"errors"
	"net"
	"net/http"
	"reflect"
	"sync"
	"sync/atomic"
	"time"

	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/servicekit"
)

const (
	DefaultReadHeaderTimeout = 10 * time.Second
	DefaultIdleTimeout       = 2 * time.Minute
	DefaultShutdownTimeout   = 30 * time.Second
	DefaultMaxHeaderBytes    = 1 << 20
)

// ServerConfig configures an HTTP server. ReadTimeout and WriteTimeout default
// to zero so streaming RPCs are not truncated; handlers must apply their own
// operation deadlines.
type ServerConfig struct {
	Addr              string
	ReadHeaderTimeout time.Duration
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration
	ShutdownTimeout   time.Duration
	MaxHeaderBytes    int
}

func (config ServerConfig) normalized() ServerConfig {
	if config.ReadHeaderTimeout == 0 {
		config.ReadHeaderTimeout = DefaultReadHeaderTimeout
	}
	if config.IdleTimeout == 0 {
		config.IdleTimeout = DefaultIdleTimeout
	}
	if config.ShutdownTimeout == 0 {
		config.ShutdownTimeout = DefaultShutdownTimeout
	}
	if config.MaxHeaderBytes == 0 {
		config.MaxHeaderBytes = DefaultMaxHeaderBytes
	}
	return config
}

func (config ServerConfig) Validate() error {
	config = config.normalized()
	if config.ReadHeaderTimeout < 0 || config.ReadTimeout < 0 || config.WriteTimeout < 0 ||
		config.IdleTimeout < 0 || config.ShutdownTimeout <= 0 || config.MaxHeaderBytes <= 0 {
		return faults.Wrap(ErrInvalidConfig, faults.CodeInvalidArgument, "invalid HTTP server configuration", faults.WithReason("invalid_http_server_config"))
	}
	return nil
}

// Server wraps http.Server with explicit lifecycle state and servicekit
// integration. It never opens a listener or exits the process implicitly.
type Server struct {
	server          *http.Server
	shutdownTimeout time.Duration
	serving         atomic.Bool
	started         atomic.Bool
	mu              sync.RWMutex
	terminalErr     error
}

func NewServer(handler http.Handler, config ServerConfig) (*Server, error) {
	if nilHandler(handler) {
		return nil, faults.Wrap(ErrNilHandler, faults.CodeInvalidArgument, "HTTP handler is required", faults.WithReason("nil_http_handler"))
	}
	config = config.normalized()
	if err := config.Validate(); err != nil {
		return nil, err
	}
	return &Server{
		server: &http.Server{
			Addr:              config.Addr,
			Handler:           handler,
			ReadHeaderTimeout: config.ReadHeaderTimeout,
			ReadTimeout:       config.ReadTimeout,
			WriteTimeout:      config.WriteTimeout,
			IdleTimeout:       config.IdleTimeout,
			MaxHeaderBytes:    config.MaxHeaderBytes,
		},
		shutdownTimeout: config.ShutdownTimeout,
	}, nil
}

func (server *Server) HTTPServer() *http.Server {
	if server == nil {
		return nil
	}
	return server.server
}

func (server *Server) Serve(listener net.Listener) error {
	if server == nil {
		return faults.New(faults.CodeFailedPrecondition, "HTTP server is not initialized", faults.WithReason("nil_http_server"))
	}
	if nilListener(listener) {
		return faults.Wrap(ErrNilListener, faults.CodeInvalidArgument, "HTTP listener is required", faults.WithReason("nil_http_listener"))
	}
	if !server.started.CompareAndSwap(false, true) {
		return faults.New(faults.CodeFailedPrecondition, "HTTP server has already been started", faults.WithReason("http_server_already_started"))
	}
	server.serving.Store(true)
	err := server.server.Serve(listener)
	server.serving.Store(false)
	if errors.Is(err, http.ErrServerClosed) {
		err = nil
	}
	server.mu.Lock()
	server.terminalErr = err
	server.mu.Unlock()
	if err != nil {
		return faults.Wrap(err, faults.CodeUnavailable, "HTTP server stopped unexpectedly", faults.WithReason("http_serve_failed"), faults.WithRetryPolicy(faults.BackoffRetry(0)))
	}
	return nil
}

func (server *Server) Shutdown(ctx context.Context) error {
	if server == nil {
		return nil
	}
	if ctx == nil {
		return faults.New(faults.CodeInvalidArgument, "shutdown context is required", faults.WithReason("nil_context"))
	}
	if !server.serving.Load() {
		return nil
	}
	if err := server.server.Shutdown(ctx); err != nil {
		code := faults.CodeOf(err)
		reason := "http_shutdown_failed"
		message := "HTTP server shutdown failed"
		switch code {
		case faults.CodeDeadlineExceeded:
			reason = "http_shutdown_timeout"
			message = "HTTP server graceful shutdown timed out"
		case faults.CodeCanceled:
			reason = "http_shutdown_canceled"
			message = "HTTP server graceful shutdown was canceled"
		case faults.CodeUnknown:
			code = faults.CodeInternal
		}
		return faults.Wrap(err, code, message, faults.WithReason(reason), faults.WithOperation("httpx.Server.Shutdown"))
	}
	return nil
}

func (server *Server) Close() error {
	if server == nil {
		return nil
	}
	if err := server.server.Close(); err != nil {
		return faults.Wrap(err, faults.CodeInternal, "HTTP server close failed", faults.WithReason("http_close_failed"))
	}
	return nil
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

// Component adapts server and an already-created listener to servicekit.
func (server *Server) Component(name string, listener net.Listener) servicekit.Component {
	return servicekit.Component{
		Name: name,
		Start: func(context.Context) error {
			if server == nil {
				return faults.New(faults.CodeFailedPrecondition, "HTTP server is not initialized", faults.WithReason("nil_http_server"))
			}
			if nilListener(listener) {
				return faults.Wrap(ErrNilListener, faults.CodeInvalidArgument, "HTTP listener is required", faults.WithReason("nil_http_listener"))
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
				shutdownCtx, cancel = context.WithTimeout(ctx, server.shutdownTimeout)
			}
			defer cancel()
			return server.Shutdown(shutdownCtx)
		},
		Liveness: func(context.Context) error {
			if err := server.Err(); err != nil {
				return err
			}
			return nil
		},
		Readiness: func(context.Context) error {
			if !server.Serving() {
				return faults.Wrap(ErrNotServing, faults.CodeUnavailable, "HTTP server is not ready", faults.WithReason("http_not_serving"), faults.WithRetryPolicy(faults.ImmediateRetry(0)))
			}
			return nil
		},
	}
}

func nilHandler(handler http.Handler) bool   { return nilValue(handler) }
func nilListener(listener net.Listener) bool { return nilValue(listener) }

func nilValue(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}
