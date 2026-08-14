// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

package interceptors

import (
	"context"
	"errors"
	"net/http"
	"runtime/debug"

	"connectrpc.com/connect"

	"mindclade.internal/libs/go/connectx"
	"mindclade.internal/libs/go/faults"
)

// Recovery contains handler panics and returns a generic internal error. The
// optional reporter receives trusted diagnostic data and is panic-isolated.
func Recovery(reporter PanicReporter) connect.Interceptor {
	return recoveryInterceptor{reporter: reporter}
}

type recoveryInterceptor struct{ reporter PanicReporter }

func (interceptor recoveryInterceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, request connect.AnyRequest) (response connect.AnyResponse, err error) {
		if request.Spec().IsClient {
			return next(ctx, request)
		}
		defer func() {
			if recovered := recover(); recovered != nil {
				if recoveredErr, ok := recovered.(error); ok && errors.Is(recoveredErr, http.ErrAbortHandler) {
					panic(recovered)
				}
				interceptor.report(ctx, request.Spec().Procedure, recovered)
				err = panicError(ctx, request.Spec().Procedure)
				response = nil
			}
		}()
		return next(ctx, request)
	}
}

func (interceptor recoveryInterceptor) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return next
}

func (interceptor recoveryInterceptor) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return func(ctx context.Context, connection connect.StreamingHandlerConn) (err error) {
		defer func() {
			if recovered := recover(); recovered != nil {
				if recoveredErr, ok := recovered.(error); ok && errors.Is(recoveredErr, http.ErrAbortHandler) {
					panic(recovered)
				}
				interceptor.report(ctx, connection.Spec().Procedure, recovered)
				err = panicError(ctx, connection.Spec().Procedure)
			}
		}()
		return next(ctx, connection)
	}
}

func (interceptor recoveryInterceptor) report(ctx context.Context, procedure string, recovered any) {
	if nilInterface(interceptor.reporter) {
		return
	}
	func() {
		defer func() { _ = recover() }()
		interceptor.reporter.ReportPanic(ctx, PanicReport{Procedure: procedure, Recovered: recovered, Stack: debug.Stack()})
	}()
}

func panicError(ctx context.Context, procedure string) error {
	operation, _ := connectx.OperationForProcedure(procedure)
	options := []faults.Option{faults.WithReason("rpc_handler_panic"), faults.WithContextMetadata(ctx)}
	if operation.Valid() {
		options = append(options, faults.WithOperation(operation.String()))
	}
	return faults.New(faults.CodeInternal, "internal server error", options...)
}
