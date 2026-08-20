// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package interceptors

import (
	"context"
	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/grpcx"
	"google.golang.org/grpc"
	"runtime/debug"
)

type PanicEvent struct {
	Context context.Context
	Method  string
	Value   any
	Stack   []byte
}
type PanicObserver interface{ ObservePanic(PanicEvent) }
type PanicObserverFunc func(PanicEvent)

func (function PanicObserverFunc) ObservePanic(event PanicEvent) { function(event) }
func UnaryRecovery(observer PanicObserver) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, request any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (response any, err error) {
		defer func() {
			if recovered := recover(); recovered != nil {
				observePanic(observer, PanicEvent{Context: ctx, Method: info.FullMethod, Value: recovered, Stack: debug.Stack()})
				options := []faults.Option{faults.WithReason("rpc_handler_panic"), faults.WithContextMetadata(ctx)}
				if method, parseErr := grpcx.ParseMethod(info.FullMethod); parseErr == nil {
					if operation, operationErr := method.Operation(); operationErr == nil {
						options = append(options, faults.WithOperation(operation.String()))
					}
				}
				err = faults.New(faults.CodeInternal, "internal server error", options...)
			}
		}()
		return handler(ctx, request)
	}
}
func StreamRecovery(observer PanicObserver) grpc.StreamServerInterceptor {
	return func(server any, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) (err error) {
		defer func() {
			if recovered := recover(); recovered != nil {
				observePanic(observer, PanicEvent{Context: stream.Context(), Method: info.FullMethod, Value: recovered, Stack: debug.Stack()})
				options := []faults.Option{faults.WithReason("rpc_handler_panic"), faults.WithContextMetadata(stream.Context())}
				if method, parseErr := grpcx.ParseMethod(info.FullMethod); parseErr == nil {
					if operation, operationErr := method.Operation(); operationErr == nil {
						options = append(options, faults.WithOperation(operation.String()))
					}
				}
				err = faults.New(faults.CodeInternal, "internal server error", options...)
			}
		}()
		return handler(server, stream)
	}
}
func observePanic(observer PanicObserver, event PanicEvent) {
	if nilInterface(observer) {
		return
	}
	func() { defer func() { _ = recover() }(); observer.ObservePanic(event) }()
}
