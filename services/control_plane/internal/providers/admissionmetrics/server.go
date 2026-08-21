// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package admissionmetrics

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/httpx"
	"go.mindclade.dev/libs/go/servicekit"
)

const (
	metricsPath                = "/metrics"
	metricsReadHeaderTimeout   = 5 * time.Second
	metricsReadTimeout         = 5 * time.Second
	metricsWriteTimeout        = 10 * time.Second
	metricsIdleTimeout         = 30 * time.Second
	metricsGatherTimeout       = 5 * time.Second
	metricsMaxHeaderBytes      = 64 << 10
	metricsMaxRequestsInFlight = 2
)

// Runtime keeps the boundary observer, private registry, pre-bound scrape
// listener, and lifecycle component in one construction result. The registry
// is deliberately not exported or installed as Prometheus's global default.
type Runtime struct {
	metrics   collectors
	handler   http.Handler
	listener  net.Listener
	server    *httpx.Server
	component servicekit.Component
}

// New constructs the boundary observer and pre-binds the dedicated metrics
// address. A listener conflict is a startup failure, never a reason to silently
// omit telemetry or expose metrics on the authenticated API listener.
func New(address string, shutdownTimeout time.Duration) (*Runtime, error) {
	address = strings.TrimSpace(address)
	if address == "" {
		return nil, faults.New(
			faults.CodeInvalidArgument,
			"admission metrics require an address",
			faults.WithReason("admission_metrics_configuration_invalid"),
			faults.WithOperation("controlplane.admissionmetrics.New"),
			faults.WithRetryPolicy(faults.NoRetry()),
		)
	}

	registry, metrics, err := newCollectors()
	if err != nil {
		return nil, faults.Wrap(err, faults.CodeInternal,
			"unable to construct admission metric collectors",
			faults.WithReason("admission_metrics_registration_failed"),
			faults.WithOperation("controlplane.admissionmetrics.New"),
			faults.WithRetryPolicy(faults.NoRetry()),
		)
	}
	exposition := promhttp.HandlerFor(registry, promhttp.HandlerOpts{
		ErrorHandling:       promhttp.HTTPErrorOnError,
		DisableCompression:  true,
		MaxRequestsInFlight: metricsMaxRequestsInFlight,
		Timeout:             metricsGatherTimeout,
	})
	handler := newMetricsHandler(exposition)
	server, err := httpx.NewServer(handler, httpx.ServerConfig{
		ReadHeaderTimeout: metricsReadHeaderTimeout,
		ReadTimeout:       metricsReadTimeout,
		WriteTimeout:      metricsWriteTimeout,
		IdleTimeout:       metricsIdleTimeout,
		ShutdownTimeout:   shutdownTimeout,
		MaxHeaderBytes:    metricsMaxHeaderBytes,
	})
	if err != nil {
		return nil, err
	}
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return nil, faults.Wrap(err, faults.CodeUnavailable,
			"unable to open the admission metrics listener",
			faults.WithReason("metrics_listener_failed"),
			faults.WithOperation("controlplane.admissionmetrics.New"),
			faults.WithField("address", address),
			faults.WithRetryPolicy(faults.BackoffRetry(0)),
		)
	}

	runtime := &Runtime{
		metrics:  metrics,
		handler:  handler,
		listener: listener,
		server:   server,
	}
	runtime.component = runtime.lifecycleComponent()
	return runtime, nil
}

// Component returns the servicekit-owned lifecycle for the scrape listener.
func (runtime *Runtime) Component() servicekit.Component {
	if runtime == nil {
		return servicekit.Component{}
	}
	return runtime.component
}

// Address returns the address selected by the pre-bound listener. It is useful
// for startup diagnostics and for port-zero qualification without exposing the
// listener itself.
func (runtime *Runtime) Address() net.Addr {
	if runtime == nil || runtime.listener == nil {
		return nil
	}
	return runtime.listener.Addr()
}

// Close releases construction-time resources when a later provider fails. At
// normal shutdown servicekit invokes the same close-safe mechanisms.
func (runtime *Runtime) Close() error {
	if runtime == nil {
		return nil
	}
	var serverErr error
	if runtime.server != nil {
		serverErr = runtime.server.Close()
	}
	return errors.Join(serverErr, closeListener(runtime.listener))
}

func (runtime *Runtime) lifecycleComponent() servicekit.Component {
	component := runtime.server.Component("admission-metrics-server", runtime.listener)
	stop := component.Stop
	component.Stop = func(ctx context.Context) error {
		return errors.Join(stop(ctx), closeListener(runtime.listener))
	}
	return component
}

func closeListener(listener net.Listener) error {
	if listener == nil {
		return nil
	}
	err := listener.Close()
	if errors.Is(err, net.ErrClosed) {
		return nil
	}
	return err
}

func newMetricsHandler(exposition http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Cache-Control", "no-store")
		writer.Header().Set("X-Content-Type-Options", "nosniff")
		if request.URL.Path != metricsPath {
			http.NotFound(writer, request)
			return
		}
		switch request.Method {
		case http.MethodGet:
			exposition.ServeHTTP(writer, request)
		case http.MethodHead:
			exposition.ServeHTTP(bodySuppressingResponseWriter{ResponseWriter: writer}, request)
		default:
			writer.Header().Set("Allow", "GET, HEAD")
			writer.WriteHeader(http.StatusMethodNotAllowed)
		}
	})
}

type bodySuppressingResponseWriter struct {
	http.ResponseWriter
}

func (writer bodySuppressingResponseWriter) Write(payload []byte) (int, error) {
	return len(payload), nil
}
