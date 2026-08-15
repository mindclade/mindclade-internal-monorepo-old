// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package connecttest

import (
	"context"
	"sync"

	"connectrpc.com/connect"
)

type Call struct {
	Procedure string
	Client    bool
}

type Recorder struct {
	mu    sync.Mutex
	calls []Call
}

func (recorder *Recorder) Interceptor() connect.Interceptor {
	return recorderInterceptor{recorder: recorder}
}
func (recorder *Recorder) Calls() []Call {
	if recorder == nil {
		return nil
	}
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	return append([]Call(nil), recorder.calls...)
}
func (recorder *Recorder) record(spec connect.Spec) {
	if recorder == nil {
		return
	}
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	recorder.calls = append(recorder.calls, Call{Procedure: spec.Procedure, Client: spec.IsClient})
}

type recorderInterceptor struct{ recorder *Recorder }

func (value recorderInterceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, request connect.AnyRequest) (connect.AnyResponse, error) {
		value.recorder.record(request.Spec())
		return next(ctx, request)
	}
}
func (value recorderInterceptor) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return func(ctx context.Context, spec connect.Spec) connect.StreamingClientConn {
		value.recorder.record(spec)
		return next(ctx, spec)
	}
}
func (value recorderInterceptor) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return func(ctx context.Context, connection connect.StreamingHandlerConn) error {
		value.recorder.record(connection.Spec())
		return next(ctx, connection)
	}
}
