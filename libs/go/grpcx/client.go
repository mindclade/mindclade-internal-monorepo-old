// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package grpcx

import (
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"mindclade.internal/libs/go/faults"
)

// NewClient creates a lazy grpc-go channel using grpc.NewClient. It performs
// no blocking dial and requires an explicit secure or insecure choice.
func NewClient(target string, config ClientConfig, additional ...grpc.DialOption) (*grpc.ClientConn, error) {
	target = strings.TrimSpace(target)
	if target == "" || len(target) > MaximumTargetLength {
		return nil, faults.Wrap(ErrInvalidConfig, faults.CodeInvalidArgument, "gRPC client target is required", faults.WithReason("missing_grpc_target"), faults.WithOperation("grpcx.NewClient"))
	}
	config = config.normalized()
	if err := config.Validate(); err != nil {
		return nil, err
	}
	options := []grpc.DialOption{
		grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(config.MaxReceiveBytes), grpc.MaxCallSendMsgSize(config.MaxSendBytes)),
	}
	if config.Insecure {
		options = append(options, grpc.WithTransportCredentials(insecure.NewCredentials()))
	} else {
		options = append(options, grpc.WithTransportCredentials(config.TransportCredentials))
	}
	if config.Authority != "" {
		options = append(options, grpc.WithAuthority(config.Authority))
	}
	if config.UserAgent != "" {
		options = append(options, grpc.WithUserAgent(config.UserAgent))
	}
	if config.DefaultServiceConfig != "" {
		options = append(options, grpc.WithDefaultServiceConfig(config.DefaultServiceConfig))
	}
	if !config.EnableRetries {
		options = append(options, grpc.WithDisableRetry())
	}
	if config.KeepaliveParameters.Time > 0 {
		options = append(options, grpc.WithKeepaliveParams(config.KeepaliveParameters))
	}
	if interceptors := nonNilUnaryClient(config.UnaryInterceptors); len(interceptors) > 0 {
		options = append(options, grpc.WithChainUnaryInterceptor(interceptors...))
	}
	if interceptors := nonNilStreamClient(config.StreamInterceptors); len(interceptors) > 0 {
		options = append(options, grpc.WithChainStreamInterceptor(interceptors...))
	}
	if !nilInterface(config.StatsHandler) {
		options = append(options, grpc.WithStatsHandler(config.StatsHandler))
	}
	for _, option := range additional {
		if option != nil {
			options = append(options, option)
		}
	}
	connection, err := grpc.NewClient(target, options...)
	if err != nil {
		return nil, faults.Wrap(err, faults.CodeUnavailable, "unable to create gRPC client", faults.WithReason("grpc_client_creation_failed"), faults.WithOperation("grpcx.NewClient"), faults.WithRetryPolicy(faults.BackoffRetry(0)))
	}
	return connection, nil
}
