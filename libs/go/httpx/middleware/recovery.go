// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package middleware

import (
	"context"
	"errors"
	"net/http"
	"runtime/debug"

	"mindclade.internal/libs/go/faults"
	"mindclade.internal/libs/go/httpx"
)

// PanicEvent contains trusted diagnostic information. Stack must never be
// rendered into an external response.
type PanicEvent struct {
	Context context.Context
	Value   any
	Stack   []byte
}

type PanicObserver interface{ ObservePanic(PanicEvent) }
type PanicObserverFunc func(PanicEvent)

func (function PanicObserverFunc) ObservePanic(event PanicEvent) { function(event) }

// Recovery converts panics that occur before response commitment into a safe
// internal error. A panic after commitment is re-thrown so net/http can abort
// the partial response rather than pretending it completed successfully.
func Recovery(observer PanicObserver) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			tracked := &trackingWriter{ResponseWriter: writer}
			defer func() {
				recovered := recover()
				if recovered == nil {
					return
				}
				if recoveredErr, ok := recovered.(error); ok && errors.Is(recoveredErr, http.ErrAbortHandler) {
					panic(recovered)
				}
				if !nilInterface(observer) {
					func() {
						defer func() { _ = recover() }()
						observer.ObservePanic(PanicEvent{Context: request.Context(), Value: recovered, Stack: debug.Stack()})
					}()
				}
				if tracked.Committed() {
					panic(recovered)
				}
				err := faults.New(faults.CodeInternal, "internal server error", faults.WithReason("handler_panic"), faults.WithContextMetadata(request.Context()))
				httpx.WriteError(request.Context(), tracked, err, request.URL.Path)
			}()
			next.ServeHTTP(tracked, request)
		})
	}
}
