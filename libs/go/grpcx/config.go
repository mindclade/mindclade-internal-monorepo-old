// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

package grpcx

import (
	"encoding/json"
	"reflect"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/stats"

	"mindclade.internal/libs/go/faults"
)

const (
	DefaultMaxReceiveBytes     = 16 << 20
	DefaultMaxSendBytes        = 64 << 20
	MaximumMessageBytes        = 256 << 20
	DefaultGracefulStopTimeout = 30 * time.Second
	MaximumTargetLength        = 4096
)

type ServerConfig struct {
	MaxReceiveBytes          int
	MaxSendBytes             int
	MaxConcurrentStreams     uint32
	ConnectionTimeout        time.Duration
	GracefulStopTimeout      time.Duration
	KeepaliveParameters      keepalive.ServerParameters
	KeepaliveEnforcement     keepalive.EnforcementPolicy
	Credentials              credentials.TransportCredentials
	RequireTransportSecurity bool
	UnaryInterceptors        []grpc.UnaryServerInterceptor
	StreamInterceptors       []grpc.StreamServerInterceptor
	StatsHandler             stats.Handler
}

func (config ServerConfig) normalized() ServerConfig {
	if config.MaxReceiveBytes == 0 {
		config.MaxReceiveBytes = DefaultMaxReceiveBytes
	}
	if config.MaxSendBytes == 0 {
		config.MaxSendBytes = DefaultMaxSendBytes
	}
	if config.GracefulStopTimeout == 0 {
		config.GracefulStopTimeout = DefaultGracefulStopTimeout
	}
	return config
}
func (config ServerConfig) Validate() error {
	config = config.normalized()
	if !validMessageLimit(config.MaxReceiveBytes) || !validMessageLimit(config.MaxSendBytes) ||
		config.ConnectionTimeout < 0 || config.GracefulStopTimeout <= 0 ||
		!validServerKeepalive(config.KeepaliveParameters, config.KeepaliveEnforcement) {
		return faults.Wrap(ErrInvalidConfig, faults.CodeInvalidArgument, "invalid gRPC server configuration", faults.WithReason("invalid_grpc_server_config"), faults.WithOperation("grpcx.ServerConfig.Validate"))
	}
	if config.RequireTransportSecurity && nilInterface(config.Credentials) {
		return faults.Wrap(ErrTransportSecurityRequired, faults.CodeFailedPrecondition, "gRPC transport security is required", faults.WithReason("grpc_transport_security_required"), faults.WithOperation("grpcx.ServerConfig.Validate"))
	}
	return nil
}
func ServerOptions(config ServerConfig) ([]grpc.ServerOption, error) {
	config = config.normalized()
	if err := config.Validate(); err != nil {
		return nil, err
	}
	options := []grpc.ServerOption{grpc.MaxRecvMsgSize(config.MaxReceiveBytes), grpc.MaxSendMsgSize(config.MaxSendBytes)}
	if config.MaxConcurrentStreams > 0 {
		options = append(options, grpc.MaxConcurrentStreams(config.MaxConcurrentStreams))
	}
	if config.ConnectionTimeout > 0 {
		options = append(options, grpc.ConnectionTimeout(config.ConnectionTimeout))
	}
	if config.KeepaliveParameters != (keepalive.ServerParameters{}) {
		options = append(options, grpc.KeepaliveParams(config.KeepaliveParameters))
	}
	if config.KeepaliveEnforcement != (keepalive.EnforcementPolicy{}) {
		options = append(options, grpc.KeepaliveEnforcementPolicy(config.KeepaliveEnforcement))
	}
	if !nilInterface(config.Credentials) {
		options = append(options, grpc.Creds(config.Credentials))
	}
	if !nilInterface(config.StatsHandler) {
		options = append(options, grpc.StatsHandler(config.StatsHandler))
	}
	if interceptors := nonNilUnaryServer(config.UnaryInterceptors); len(interceptors) > 0 {
		options = append(options, grpc.ChainUnaryInterceptor(interceptors...))
	}
	if interceptors := nonNilStreamServer(config.StreamInterceptors); len(interceptors) > 0 {
		options = append(options, grpc.ChainStreamInterceptor(interceptors...))
	}
	return options, nil
}

type ClientConfig struct {
	TransportCredentials credentials.TransportCredentials
	Insecure             bool
	Authority            string
	UserAgent            string
	DefaultServiceConfig string
	EnableRetries        bool
	MaxReceiveBytes      int
	MaxSendBytes         int
	KeepaliveParameters  keepalive.ClientParameters
	UnaryInterceptors    []grpc.UnaryClientInterceptor
	StreamInterceptors   []grpc.StreamClientInterceptor
	StatsHandler         stats.Handler
}

func (config ClientConfig) normalized() ClientConfig {
	if config.MaxReceiveBytes == 0 {
		config.MaxReceiveBytes = DefaultMaxReceiveBytes
	}
	if config.MaxSendBytes == 0 {
		config.MaxSendBytes = DefaultMaxSendBytes
	}
	config.Authority = strings.TrimSpace(config.Authority)
	config.UserAgent = strings.TrimSpace(config.UserAgent)
	config.DefaultServiceConfig = strings.TrimSpace(config.DefaultServiceConfig)
	return config
}
func (config ClientConfig) Validate() error {
	config = config.normalized()
	if !validMessageLimit(config.MaxReceiveBytes) || !validMessageLimit(config.MaxSendBytes) || !validClientKeepalive(config.KeepaliveParameters) {
		return faults.Wrap(ErrInvalidConfig, faults.CodeInvalidArgument, "invalid gRPC client configuration", faults.WithReason("invalid_grpc_client_config"), faults.WithOperation("grpcx.ClientConfig.Validate"))
	}
	if config.Insecure && !nilInterface(config.TransportCredentials) {
		return faults.Wrap(ErrInvalidConfig, faults.CodeInvalidArgument, "gRPC client security configuration is ambiguous", faults.WithReason("ambiguous_grpc_transport_security"), faults.WithOperation("grpcx.ClientConfig.Validate"))
	}
	if !config.Insecure && nilInterface(config.TransportCredentials) {
		return faults.Wrap(ErrTransportSecurityRequired, faults.CodeFailedPrecondition, "gRPC transport credentials are required", faults.WithReason("grpc_transport_security_required"), faults.WithOperation("grpcx.ClientConfig.Validate"))
	}
	if config.DefaultServiceConfig != "" {
		var serviceConfig map[string]any
		if err := json.Unmarshal([]byte(config.DefaultServiceConfig), &serviceConfig); err != nil || serviceConfig == nil {
			return faults.Wrap(ErrInvalidConfig, faults.CodeInvalidArgument, "invalid gRPC default service configuration", faults.WithReason("invalid_grpc_service_config"), faults.WithOperation("grpcx.ClientConfig.Validate"))
		}
	}
	return nil
}

func validMessageLimit(value int) bool { return value >= 1 && value <= MaximumMessageBytes }

func validServerKeepalive(parameters keepalive.ServerParameters, enforcement keepalive.EnforcementPolicy) bool {
	return parameters.MaxConnectionIdle >= 0 &&
		parameters.MaxConnectionAge >= 0 &&
		parameters.MaxConnectionAgeGrace >= 0 &&
		parameters.Time >= 0 &&
		parameters.Timeout >= 0 &&
		enforcement.MinTime >= 0
}

func validClientKeepalive(parameters keepalive.ClientParameters) bool {
	return parameters.Time >= 0 && parameters.Timeout >= 0
}
func nilInterface(value any) bool {
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
func nonNilUnaryServer(input []grpc.UnaryServerInterceptor) []grpc.UnaryServerInterceptor {
	output := make([]grpc.UnaryServerInterceptor, 0, len(input))
	for _, v := range input {
		if v != nil {
			output = append(output, v)
		}
	}
	return output
}
func nonNilStreamServer(input []grpc.StreamServerInterceptor) []grpc.StreamServerInterceptor {
	output := make([]grpc.StreamServerInterceptor, 0, len(input))
	for _, v := range input {
		if v != nil {
			output = append(output, v)
		}
	}
	return output
}
func nonNilUnaryClient(input []grpc.UnaryClientInterceptor) []grpc.UnaryClientInterceptor {
	output := make([]grpc.UnaryClientInterceptor, 0, len(input))
	for _, v := range input {
		if v != nil {
			output = append(output, v)
		}
	}
	return output
}
func nonNilStreamClient(input []grpc.StreamClientInterceptor) []grpc.StreamClientInterceptor {
	output := make([]grpc.StreamClientInterceptor, 0, len(input))
	for _, v := range input {
		if v != nil {
			output = append(output, v)
		}
	}
	return output
}
