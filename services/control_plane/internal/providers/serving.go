// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package providers

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"go.mindclade.dev/libs/go/auth"
	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/httpx"
	"go.mindclade.dev/libs/go/httpx/health"
	"go.mindclade.dev/libs/go/httpx/middleware"
	"go.mindclade.dev/libs/go/observability"
	"go.mindclade.dev/libs/go/requestmeta"
	"go.mindclade.dev/libs/go/servicekit"
	"go.mindclade.dev/libs/go/servicekit/production"
	"go.mindclade.dev/services/control_plane/internal/bootstrap"
	"go.mindclade.dev/services/control_plane/internal/config"
	"go.mindclade.dev/services/control_plane/internal/transport"
)

// maximumRequestBody bounds decoded request bodies. Blob content travels
// through the object store, never through the control-plane API, so this stays
// small deliberately.
const maximumRequestBody = 1 << 20

// prober forwards health probes to the assembled service. The HTTP handler is
// built before the service exists, so the runtime is attached by the bootstrap
// Bind hook and read atomically from request goroutines.
type prober struct {
	runtime atomic.Pointer[production.Runtime]
}

func (value *prober) bind(runtime *production.Runtime) error {
	if value == nil || runtime == nil {
		return faults.New(
			faults.CodeInvalidArgument,
			"health prober cannot bind a nil runtime",
			faults.WithReason("nil_bound_runtime"),
			faults.WithOperation("controlplane.providers.prober.bind"),
			faults.WithRetryPolicy(faults.NoRetry()),
		)
	}
	value.runtime.Store(runtime)
	return nil
}

func (value *prober) Liveness(ctx context.Context) servicekit.ProbeReport {
	runtime := value.runtime.Load()
	if runtime == nil || runtime.Service() == nil {
		return unboundReport()
	}
	return runtime.Service().Liveness(ctx)
}

func (value *prober) Readiness(ctx context.Context) servicekit.ProbeReport {
	runtime := value.runtime.Load()
	if runtime == nil || runtime.Service() == nil {
		return unboundReport()
	}
	return runtime.Service().Readiness(ctx)
}

// unboundReport fails closed for the window between the listener opening and
// the runtime being bound, so an orchestrator never sees a premature ready.
func unboundReport() servicekit.ProbeReport {
	now := time.Now().UTC()
	return servicekit.ProbeReport{
		OK:        false,
		CheckedAt: now,
		Results:   []servicekit.ProbeResult{{Name: "runtime", OK: false, CheckedAt: now}},
	}
}

// serving is the assembled inbound transport for one process.
type serving struct {
	components bootstrap.Components
	bind       func(*production.Runtime) error
}

func newServing(settings config.Settings, telemetry *observability.Runtime, authenticator auth.Authenticator) (serving, error) {
	listener, err := net.Listen("tcp", settings.HTTPAddress)
	if err != nil {
		return serving{}, faults.Wrap(err, faults.CodeUnavailable,
			"unable to open the control-plane HTTP listener",
			faults.WithReason("http_listener_failed"),
			faults.WithOperation("controlplane.providers.newServing"),
			faults.WithField("address", settings.HTTPAddress),
		)
	}
	value := &prober{}
	handler := newHandler(value, telemetry, authenticator)
	adapter, err := transport.NewHTTP("http-server", handler, listener, httpx.ServerConfig{
		ReadTimeout:     30 * time.Second,
		WriteTimeout:    60 * time.Second,
		ShutdownTimeout: settings.DrainTimeout,
	})
	if err != nil {
		_ = listener.Close()
		return serving{}, err
	}
	components, err := transport.Bundle{HTTP: adapter}.Components()
	if err != nil {
		_ = listener.Close()
		return serving{}, err
	}
	return serving{components: components, bind: value.bind}, nil
}

// newHandler mounts health outside the authenticated stack, because an
// orchestrator probing liveness holds no credential, and everything else
// inside it. Domain routes are registered by the API surface that owns them;
// until one is mounted this handler correctly answers 404 for every path.
func newHandler(value *prober, telemetry *observability.Runtime, authenticator auth.Authenticator) http.Handler {
	api := http.NewServeMux()
	guarded := middleware.Server(api, middleware.StackConfig{
		OperationResolver: middleware.OperationResolverFunc(resolveOperation),
		AccessObserver:    accessObserver(telemetry),
		PanicObserver:     panicObserver(telemetry),
		Security:          middleware.SecurityHeadersConfig{},
		MaximumBodyBytes:  maximumRequestBody,
		Authentication:    &middleware.AuthenticationConfig{Authenticator: authenticator},
		Authorization:     &middleware.AuthorizationConfig{Authorizer: auth.PermissionAuthorizer{}},
		Additional:        []middleware.Middleware{transport.Preconditions()},
	})

	root := http.NewServeMux()
	root.Handle("/livez", health.NewHandler(value, health.Config{}))
	root.Handle("/readyz", health.NewHandler(value, health.Config{}))
	root.Handle("/", guarded)
	return root
}

// resolveOperation names the request before routing has happened, so only the
// method is known. Handlers narrow this with requestmeta.WithOperation once
// they have matched a route.
func resolveOperation(request *http.Request) (requestmeta.Operation, error) {
	method := strings.ToLower(strings.TrimSpace(request.Method))
	if method == "" {
		method = "unknown"
	}
	return requestmeta.ParseOperation("controlplane." + method)
}

func accessObserver(telemetry *observability.Runtime) middleware.AccessObserver {
	return middleware.AccessObserverFunc(func(event middleware.AccessEvent) {
		observability.LoggerFromContext(event.Context, telemetry.Logger()).LogAttrs(
			event.Context, slog.LevelInfo, "http access",
			slog.String("method", event.Method),
			slog.String("path", event.Path),
			slog.Int("status", event.Status),
			slog.Int64("bytes", event.Bytes),
			slog.Duration("duration", event.Duration),
		)
	})
}

func panicObserver(telemetry *observability.Runtime) middleware.PanicObserver {
	return middleware.PanicObserverFunc(func(event middleware.PanicEvent) {
		observability.LoggerFromContext(event.Context, telemetry.Logger()).LogAttrs(
			event.Context, slog.LevelError, "http handler panic",
			slog.String("stack", string(event.Stack)),
		)
	})
}
