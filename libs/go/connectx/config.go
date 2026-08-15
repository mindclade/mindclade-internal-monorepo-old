// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package connectx

import (
	"net/url"
	"strings"

	"connectrpc.com/connect"

	"mindclade.internal/libs/go/faults"
)

const MaximumMessageBytes = 1 << 30

type Protocol string

const (
	ProtocolConnect Protocol = "connect"
	ProtocolGRPC    Protocol = "grpc"
	ProtocolGRPCWeb Protocol = "grpc_web"
)

func ParseProtocol(value string) (Protocol, error) {
	protocol := Protocol(strings.ToLower(strings.TrimSpace(value)))
	if protocol == "" {
		return ProtocolConnect, nil
	}
	if !protocol.Valid() {
		return "", faults.Wrap(ErrInvalidProtocol, faults.CodeInvalidArgument, "invalid Connect client protocol", faults.WithReason("invalid_connect_protocol"), faults.WithOperation("connectx.ParseProtocol"))
	}
	return protocol, nil
}

func (protocol Protocol) Valid() bool {
	switch protocol {
	case "", ProtocolConnect, ProtocolGRPC, ProtocolGRPCWeb:
		return true
	default:
		return false
	}
}

// HandlerConfig configures generated Connect handlers. Zero limits preserve
// Connect's defaults; production services should normally set explicit limits.
type HandlerConfig struct {
	ReadMaxBytes          int
	SendMaxBytes          int
	CompressMinBytes      int
	RequireProtocolHeader bool
	Interceptors          []connect.Interceptor
	ExtraOptions          []connect.HandlerOption
}

// ClientConfig configures generated Connect clients.
type ClientConfig struct {
	Protocol         Protocol
	ReadMaxBytes     int
	SendMaxBytes     int
	CompressMinBytes int
	SendGzip         bool
	EnableHTTPGet    bool
	UseProtoJSON     bool
	Interceptors     []connect.Interceptor
	ExtraOptions     []connect.ClientOption
}

func HandlerOptions(config HandlerConfig) ([]connect.HandlerOption, error) {
	if err := validateLimits(config.ReadMaxBytes, config.SendMaxBytes, config.CompressMinBytes); err != nil {
		return nil, err
	}
	options := make([]connect.HandlerOption, 0, 5+len(config.ExtraOptions))
	if config.ReadMaxBytes > 0 {
		options = append(options, connect.WithReadMaxBytes(config.ReadMaxBytes))
	}
	if config.SendMaxBytes > 0 {
		options = append(options, connect.WithSendMaxBytes(config.SendMaxBytes))
	}
	if config.CompressMinBytes > 0 {
		options = append(options, connect.WithCompressMinBytes(config.CompressMinBytes))
	}
	if config.RequireProtocolHeader {
		options = append(options, connect.WithRequireConnectProtocolHeader())
	}
	if len(config.Interceptors) > 0 {
		options = append(options, connect.WithInterceptors(config.Interceptors...))
	}
	options = append(options, nonNilHandlerOptions(config.ExtraOptions)...)
	return options, nil
}

func ClientOptions(config ClientConfig) ([]connect.ClientOption, error) {
	if !config.Protocol.Valid() {
		return nil, faults.Wrap(ErrInvalidProtocol, faults.CodeInvalidArgument, "invalid Connect client protocol", faults.WithReason("invalid_connect_protocol"), faults.WithOperation("connectx.ClientOptions"))
	}
	if err := validateLimits(config.ReadMaxBytes, config.SendMaxBytes, config.CompressMinBytes); err != nil {
		return nil, err
	}
	options := make([]connect.ClientOption, 0, 8+len(config.ExtraOptions))
	switch config.Protocol {
	case ProtocolGRPC:
		options = append(options, connect.WithGRPC())
	case ProtocolGRPCWeb:
		options = append(options, connect.WithGRPCWeb())
	}
	if config.ReadMaxBytes > 0 {
		options = append(options, connect.WithReadMaxBytes(config.ReadMaxBytes))
	}
	if config.SendMaxBytes > 0 {
		options = append(options, connect.WithSendMaxBytes(config.SendMaxBytes))
	}
	if config.CompressMinBytes > 0 {
		options = append(options, connect.WithCompressMinBytes(config.CompressMinBytes))
	}
	if config.SendGzip {
		options = append(options, connect.WithSendGzip())
	}
	if config.EnableHTTPGet {
		if config.Protocol == ProtocolGRPC || config.Protocol == ProtocolGRPCWeb {
			return nil, faults.Wrap(ErrInvalidConfig, faults.CodeInvalidArgument, "HTTP GET is available only for the Connect protocol", faults.WithReason("http_get_protocol_conflict"), faults.WithOperation("connectx.ClientOptions"))
		}
		options = append(options, connect.WithHTTPGet())
	}
	if config.UseProtoJSON {
		options = append(options, connect.WithProtoJSON())
	}
	if len(config.Interceptors) > 0 {
		options = append(options, connect.WithInterceptors(config.Interceptors...))
	}
	options = append(options, nonNilClientOptions(config.ExtraOptions)...)
	return options, nil
}

func validateLimits(values ...int) error {
	for _, value := range values {
		if value < 0 || value > MaximumMessageBytes {
			return faults.Wrap(ErrInvalidConfig, faults.CodeInvalidArgument, "invalid Connect size limit", faults.WithReason("invalid_connect_size_limit"), faults.WithOperation("connectx.validateLimits"), faults.WithField("limit", value))
		}
	}
	return nil
}

func nonNilHandlerOptions(options []connect.HandlerOption) []connect.HandlerOption {
	filtered := make([]connect.HandlerOption, 0, len(options))
	for _, option := range options {
		if option != nil {
			filtered = append(filtered, option)
		}
	}
	return filtered
}

func nonNilClientOptions(options []connect.ClientOption) []connect.ClientOption {
	filtered := make([]connect.ClientOption, 0, len(options))
	for _, option := range options {
		if option != nil {
			filtered = append(filtered, option)
		}
	}
	return filtered
}

// NormalizeBaseURL returns a generated-client base URL without a trailing
// slash. It deliberately does not infer schemes or rewrite hosts.
func NormalizeBaseURL(value string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed == nil || (parsed.Scheme != "https" && parsed.Scheme != "http") ||
		parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", faults.Wrap(ErrInvalidConfig, faults.CodeInvalidArgument, "invalid Connect base URL", faults.WithReason("invalid_connect_base_url"), faults.WithOperation("connectx.NormalizeBaseURL"))
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	parsed.RawPath = ""
	return parsed.String(), nil
}
