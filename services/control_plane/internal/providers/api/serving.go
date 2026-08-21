// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package api

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	"go.mindclade.dev/libs/go/auth"
	"go.mindclade.dev/libs/go/connectx"
	connecthealth "go.mindclade.dev/libs/go/connectx/health"
	connectinterceptors "go.mindclade.dev/libs/go/connectx/interceptors"
	connectotel "go.mindclade.dev/libs/go/connectx/otel"
	connectreflection "go.mindclade.dev/libs/go/connectx/reflection"
	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/grpcx"
	grpccredentials "go.mindclade.dev/libs/go/grpcx/credentials"
	grpchealth "go.mindclade.dev/libs/go/grpcx/health"
	grpcinterceptors "go.mindclade.dev/libs/go/grpcx/interceptors"
	grpcotel "go.mindclade.dev/libs/go/grpcx/otel"
	grpcreflection "go.mindclade.dev/libs/go/grpcx/reflection"
	"go.mindclade.dev/libs/go/httpx"
	"go.mindclade.dev/libs/go/httpx/health"
	"go.mindclade.dev/libs/go/httpx/middleware"
	"go.mindclade.dev/libs/go/observability"
	"go.mindclade.dev/libs/go/requestmeta"
	"go.mindclade.dev/libs/go/servicekit/production"
	"go.mindclade.dev/services/control_plane/internal/bootstrap"
	"go.mindclade.dev/services/control_plane/internal/config"
	"go.mindclade.dev/services/control_plane/internal/providers/admissionmetrics"
	"go.mindclade.dev/services/control_plane/internal/transport"
)

// maximumRequestBody bounds decoded request bodies. Blob content travels
// through the object store, never through the control-plane API.
const maximumRequestBody = 1 << 20

// healthService is the one RPC service this process serves today. It is a real
// service, not a placeholder: orchestrators probe it and grpcurl reflects it.
// Domain services add their own names here as they are generated and mounted;
// advertising a name that is not served would make reflection lie.
const healthService = "grpc.health.v1.Health"

// serving is the assembled inbound transport for the API process: HTTP, the
// Connect handlers sharing its listener, and gRPC on its own.
type serving struct {
	components bootstrap.Components
	bind       func(*production.Runtime) error
}

func newServing(settings config.Settings, telemetry *observability.Runtime, authenticator auth.Authenticator, admissions admissionEngine, metrics *admissionmetrics.Runtime) (result serving, err error) {
	value := &transport.Prober{}

	closers := make([]func(), 0, 2)
	defer func() {
		if err == nil {
			return
		}
		for index := len(closers) - 1; index >= 0; index-- {
			closers[index]()
		}
	}()

	httpListener, err := net.Listen("tcp", settings.HTTPAddress)
	if err != nil {
		return serving{}, listenerFailed(err, "http", settings.HTTPAddress)
	}
	closers = append(closers, func() { _ = httpListener.Close() })

	handler, connectMounted, err := newHandler(value, telemetry, authenticator, admissions, metrics)
	if err != nil {
		return serving{}, err
	}
	httpAdapter, err := transport.NewHTTP("http-server", handler, httpListener, httpx.ServerConfig{
		ReadTimeout:     30 * time.Second,
		WriteTimeout:    60 * time.Second,
		ShutdownTimeout: settings.DrainTimeout,
	})
	if err != nil {
		return serving{}, err
	}

	grpcListener, err := net.Listen("tcp", settings.GRPCAddress)
	if err != nil {
		return serving{}, listenerFailed(err, "grpc", settings.GRPCAddress)
	}
	closers = append(closers, func() { _ = grpcListener.Close() })

	grpcAdapter, err := newGRPC(settings, value, authenticator)
	if err != nil {
		return serving{}, err
	}
	adapter, err := transport.NewGRPC("grpc-server", grpcListener, grpcAdapter.config, grpcAdapter.registrars...)
	if err != nil {
		return serving{}, err
	}

	components, err := transport.Bundle{HTTP: httpAdapter, GRPC: adapter, Connect: connectMounted}.Components()
	if err != nil {
		return serving{}, err
	}
	return serving{components: components, bind: value.Bind}, nil
}

// newHandler builds the HTTP surface. Health and reflection sit outside the
// authenticated stack because an orchestrator probing liveness and an operator
// running grpcurl both hold no credential. Everything else sits inside it.
//
// The second return reports whether Connect was mounted, which is what makes
// the process claim CapabilityConnect.
func newHandler(
	value *transport.Prober,
	telemetry *observability.Runtime,
	authenticator auth.Authenticator,
	admissions admissionEngine,
	metrics *admissionmetrics.Runtime,
) (http.Handler, bool, error) {
	root := http.NewServeMux()
	root.Handle("/livez", health.NewHandler(value, health.Config{}))
	root.Handle("/readyz", health.NewHandler(value, health.Config{}))

	options, err := connectOptions(telemetry, authenticator)
	if err != nil {
		return nil, false, err
	}

	checker, err := connecthealth.NewChecker(value, healthService)
	if err != nil {
		return nil, false, err
	}
	healthPath, healthHandler := connecthealth.NewHandler(checker, options...)
	if _, err := transport.MountConnect(root, transport.ConnectMount{Path: healthPath, Handler: healthHandler}); err != nil {
		return nil, false, err
	}

	// v1alpha is off: it exists for clients predating the v1 reflection
	// service, and this API has no such clients to keep working.
	mounts, err := connectreflection.Handlers([]string{healthService}, false, options...)
	if err != nil {
		return nil, false, err
	}
	if err := connectreflection.Register(root, mounts...); err != nil {
		return nil, false, err
	}

	api, err := newAdmissionMux(admissions, metrics)
	if err != nil {
		return nil, false, err
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
			Resolver:       middleware.AuthorizationResolverFunc(resolveAdmissionAuthorization),
			RequireMapping: true,
		},
		Additional: []middleware.Middleware{transport.Preconditions()},
	})
	guarded = metrics.Middleware(guarded)
	root.Handle("/", guarded)

	// Deliberately not wrapped in httpx/otel. The Connect handlers above are
	// already instrumented by connectx/otel, and wrapping the whole tree in
	// otelhttp would emit a second span for every Connect request. See the
	// httpx/otel waiver in libs/go/UNCONSUMED.toml.
	return root, true, nil
}

// connectOptions is the canonical Connect handler stack. Order matters:
// recovery is outermost so a panic in any later interceptor still becomes a
// structured error, and fault translation sits above the rest so every
// libs/go/faults code reaches the client as the right Connect code.
//
// Authentication is optional here because these options are shared with health
// and reflection, which must answer an unauthenticated caller. A credential
// that IS presented is still verified, so a bad one is rejected rather than
// ignored.
func connectOptions(telemetry *observability.Runtime, authenticator auth.Authenticator) ([]connect.HandlerOption, error) {
	tracing, err := connectotel.NewInterceptor()
	if err != nil {
		return nil, err
	}
	return []connect.HandlerOption{connect.WithInterceptors(
		connectinterceptors.Recovery(panicReporter(telemetry)),
		tracing,
		connectinterceptors.FaultTranslation(),
		connectinterceptors.RequestMetadata(),
		connectinterceptors.Validation(),
		connectinterceptors.Authentication(connectinterceptors.AuthenticationConfig{
			Authenticator: authenticator,
			Optional:      true,
		}),
	)}, nil
}

// grpcServer is the assembled gRPC configuration and the registrations that
// must run before the server starts.
type grpcServer struct {
	config     grpcx.ServerConfig
	registrars []transport.GRPCRegistrar
}

func newGRPC(settings config.Settings, value *transport.Prober, authenticator auth.Authenticator) (grpcServer, error) {
	credentials, err := transportCredentials(settings)
	if err != nil {
		return grpcServer{}, err
	}
	configuration := grpcx.ServerConfig{
		ConnectionTimeout:   30 * time.Second,
		GracefulStopTimeout: settings.DrainTimeout,
		Credentials:         credentials,
		// Plaintext is refused whenever the deployment configured TLS. Without
		// a certificate the process serves plaintext and the deployment is
		// expected to terminate TLS ahead of it.
		RequireTransportSecurity: credentials != nil,
		StatsHandler:             grpcotel.NewServerStatsHandler(),
		UnaryInterceptors: []grpc.UnaryServerInterceptor{
			grpcinterceptors.UnaryAuthentication(grpcinterceptors.AuthenticationConfig{
				Authenticator: authenticator,
				Optional:      true,
			}),
			grpcinterceptors.UnaryAuthorization(grpcinterceptors.AuthorizationConfig{
				Authorizer: auth.PermissionAuthorizer{},
				Resolver:   grpcinterceptors.AuthorizationResolverFunc(resolveGRPCAuthorization),
			}),
		},
		StreamInterceptors: []grpc.StreamServerInterceptor{
			grpcinterceptors.StreamAuthentication(grpcinterceptors.AuthenticationConfig{
				Authenticator: authenticator,
				Optional:      true,
			}),
			grpcinterceptors.StreamAuthorization(grpcinterceptors.AuthorizationConfig{
				Authorizer: auth.PermissionAuthorizer{},
				Resolver:   grpcinterceptors.AuthorizationResolverFunc(resolveGRPCAuthorization),
			}),
		},
	}
	return grpcServer{
		config: configuration,
		registrars: []transport.GRPCRegistrar{
			func(server *grpc.Server) error {
				_, err := grpchealth.Register(server, value, grpchealth.Config{Services: []string{healthService}})
				return err
			},
			func(server *grpc.Server) error {
				// v1 only, for the same reason the Connect surface skips
				// v1alpha.
				return grpcreflection.Register(server, grpcreflection.Config{V1Only: true})
			},
		},
	}, nil
}

// resolveGRPCAuthorization maps a gRPC method onto the permission it needs.
// Only health and reflection are served today and neither is permission-gated,
// so nothing maps yet. RequireMapping stays false: an unmapped method is
// allowed through to a handler that does not exist and answers Unimplemented,
// which is the honest response. Domain services add their mappings here, and
// flipping RequireMapping to true is the step that makes an unmapped method a
// configuration error instead.
func resolveGRPCAuthorization(string, any) (grpcinterceptors.AuthorizationTarget, bool, error) {
	return grpcinterceptors.AuthorizationTarget{}, false, nil
}

// transportCredentials builds server TLS when the deployment configured a
// certificate pair, and mutual TLS when it also configured a client CA. A
// certificate without a key, or a key without a certificate, is a deployment
// error rather than a reason to silently serve plaintext.
func transportCredentials(settings config.Settings) (credentials.TransportCredentials, error) {
	certificate := strings.TrimSpace(settings.GRPCTLSCertificate)
	key := strings.TrimSpace(settings.GRPCTLSKey)
	clientCA := strings.TrimSpace(settings.GRPCTLSClientCA)

	if certificate == "" && key == "" {
		if clientCA != "" {
			return nil, faults.New(
				faults.CodeFailedPrecondition,
				"gRPC client CA requires a server certificate and key",
				faults.WithReason("client_ca_without_server_certificate"),
				faults.WithOperation("controlplane.api.transportCredentials"),
				faults.WithRetryPolicy(faults.NoRetry()),
			)
		}
		return nil, nil
	}
	if certificate == "" || key == "" {
		return nil, faults.New(
			faults.CodeFailedPrecondition,
			"gRPC TLS requires both a certificate and a key",
			faults.WithReason("incomplete_grpc_tls_pair"),
			faults.WithOperation("controlplane.api.transportCredentials"),
			faults.WithRetryPolicy(faults.NoRetry()),
		)
	}
	pair, err := grpccredentials.LoadCertificate(certificate, key)
	if err != nil {
		return nil, err
	}
	configuration := grpccredentials.ServerConfig{Certificate: pair}
	if clientCA != "" {
		pool, err := grpccredentials.LoadCertPool(clientCA)
		if err != nil {
			return nil, err
		}
		configuration.ClientCAs = pool
		configuration.RequireClientCertificate = true
	}
	return grpccredentials.NewServer(configuration)
}

// panicReporter routes a recovered Connect panic to the process logger. The
// report carries a stack and the recovered value, neither of which may reach
// the client; the interceptor already substitutes a generic error for that.
func panicReporter(telemetry *observability.Runtime) connectinterceptors.PanicReporter {
	return connectinterceptors.PanicReporterFunc(func(ctx context.Context, report connectinterceptors.PanicReport) {
		observability.LoggerFromContext(ctx, telemetry.Logger()).LogAttrs(
			ctx, slog.LevelError, "connect handler panic",
			slog.String("procedure", report.Procedure),
			slog.String("stack", string(report.Stack)),
		)
	})
}

func listenerFailed(err error, kind, address string) error {
	return faults.Wrap(err, faults.CodeUnavailable,
		"unable to open the control-plane listener",
		faults.WithReason(kind+"_listener_failed"),
		faults.WithOperation("controlplane.api.newServing"),
		faults.WithField("address", address),
	)
}

// resolveOperation names the request before routing has happened, so only the
// method is known.
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

var _ connectx.Mux = (*http.ServeMux)(nil)
