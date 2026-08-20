// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package interceptors

import (
	"context"
	"go.mindclade.dev/libs/go/faults"
	"google.golang.org/grpc"
)

type validator interface{ Validate() error }
type validatorAll interface{ ValidateAll() error }

func UnaryValidation() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, request any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if err := validateMessage(request); err != nil {
			return nil, err
		}
		return handler(ctx, request)
	}
}
func StreamValidation() grpc.StreamServerInterceptor {
	return func(server any, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		return handler(server, &validatingServerStream{ServerStream: stream})
	}
}

type validatingServerStream struct{ grpc.ServerStream }

func (stream *validatingServerStream) RecvMsg(message any) error {
	if err := stream.ServerStream.RecvMsg(message); err != nil {
		return err
	}
	return validateMessage(message)
}
func validateMessage(message any) error {
	if message == nil {
		return nil
	}
	var err error
	switch typed := message.(type) {
	case validatorAll:
		err = typed.ValidateAll()
	case validator:
		err = typed.Validate()
	}
	if err == nil {
		return nil
	}
	return faults.Wrap(err, faults.CodeInvalidArgument, "request validation failed", faults.WithReason("request_validation_failed"))
}
