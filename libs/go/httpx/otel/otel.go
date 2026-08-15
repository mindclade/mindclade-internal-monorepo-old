// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package otel

import (
	"net/http"
	"reflect"
	"strings"
	"unicode/utf8"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"mindclade.internal/libs/go/faults"
	"mindclade.internal/libs/go/httpx"
)

const maximumOperationLength = 256

// NewHandler wraps an ordinary HTTP handler with the official OpenTelemetry
// HTTP instrumentation. The caller owns providers, propagators, filters, and
// span-name policy through otelhttp options.
func NewHandler(handler http.Handler, operation string, options ...otelhttp.Option) (http.Handler, error) {
	if nilInterface(handler) {
		return nil, faults.Wrap(
			httpx.ErrNilHandler,
			faults.CodeInvalidArgument,
			"HTTP telemetry handler is required",
			faults.WithReason("nil_http_telemetry_handler"),
			faults.WithOperation("httpx/otel.NewHandler"),
		)
	}
	operation = strings.TrimSpace(operation)
	if operation == "" || len(operation) > maximumOperationLength || !utf8.ValidString(operation) {
		return nil, faults.New(
			faults.CodeInvalidArgument,
			"invalid HTTP telemetry operation",
			faults.WithReason("invalid_http_telemetry_operation"),
			faults.WithOperation("httpx/otel.NewHandler"),
		)
	}
	return otelhttp.NewHandler(handler, operation, options...), nil
}

// NewTransport wraps base with the official OpenTelemetry HTTP transport. A
// nil or typed-nil base delegates to net/http's default transport.
func NewTransport(base http.RoundTripper, options ...otelhttp.Option) http.RoundTripper {
	if nilInterface(base) {
		base = http.DefaultTransport
	}
	return otelhttp.NewTransport(base, options...)
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
