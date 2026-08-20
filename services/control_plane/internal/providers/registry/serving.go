// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package registry

import (
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"go.mindclade.dev/libs/go/auth"
	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/httpx"
	"go.mindclade.dev/libs/go/httpx/health"
	"go.mindclade.dev/libs/go/httpx/middleware"
	"go.mindclade.dev/libs/go/observability"
	"go.mindclade.dev/libs/go/requestmeta"
	"go.mindclade.dev/libs/go/servicekit/production"
	"go.mindclade.dev/services/control_plane/internal/bootstrap"
	"go.mindclade.dev/services/control_plane/internal/config"
	"go.mindclade.dev/services/control_plane/internal/transport"
)

// maximumRequestBody bounds decoded request bodies. Blob content travels
// through the object store, never through the control-plane API, so this stays
// small deliberately.
const maximumRequestBody = 1 << 20

// serving is the assembled inbound transport for one process.
type serving struct {
	components bootstrap.Components
	bind       func(*production.Runtime) error
}

func newServing(settings config.Settings, telemetry *observability.Runtime, authenticator auth.Authenticator, domains domains) (serving, error) {
	listener, err := net.Listen("tcp", settings.HTTPAddress)
	if err != nil {
		return serving{}, faults.Wrap(err, faults.CodeUnavailable,
			"unable to open the control-plane HTTP listener",
			faults.WithReason("http_listener_failed"),
			faults.WithOperation("controlplane.registry.newServing"),
			faults.WithField("address", settings.HTTPAddress),
		)
	}
	value := &transport.Prober{}
	handler, err := newHandler(value, telemetry, authenticator, domains)
	if err != nil {
		_ = listener.Close()
		return serving{}, err
	}
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
	return serving{components: components, bind: value.Bind}, nil
}

// newHandler mounts health outside the authenticated stack, because an
// orchestrator probing liveness holds no credential, and everything else
// inside it. Domain routes are registered by the API surface that owns them;
// until one is mounted this handler correctly answers 404 for every path.
func newHandler(value *transport.Prober, telemetry *observability.Runtime, authenticator auth.Authenticator, domains domains) (http.Handler, error) {
	api, err := newRegistryMux(domains)
	if err != nil {
		return nil, err
	}
	guarded := middleware.Server(api, middleware.StackConfig{
		OperationResolver: middleware.OperationResolverFunc(resolveOperation),
		AccessObserver:    accessObserver(telemetry),
		PanicObserver:     panicObserver(telemetry),
		Security:          middleware.SecurityHeadersConfig{},
		MaximumBodyBytes:  maximumRequestBody,
		Authentication:    &middleware.AuthenticationConfig{Authenticator: authenticator},
		Authorization: &middleware.AuthorizationConfig{
			Authorizer:     auth.PermissionAuthorizer{},
			Resolver:       middleware.AuthorizationResolverFunc(resolveAuthorization),
			RequireMapping: true,
		},
		Additional: []middleware.Middleware{transport.Preconditions()},
	})

	root := http.NewServeMux()
	root.Handle("/livez", health.NewHandler(value, health.Config{}))
	root.Handle("/readyz", health.NewHandler(value, health.Config{}))
	root.Handle("/", guarded)
	return root, nil
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
