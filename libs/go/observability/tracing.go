// Copyright 2026 Mindclade. All rights reserved.
// Confidential and proprietary.

package observability

import (
	"context"
	"encoding/hex"
	"strings"

	"mindclade.internal/libs/go/faults"
)

// TraceContext contains only correlation identifiers needed by logs and
// diagnostics. Span creation remains the responsibility of the injected
// tracing provider, such as OpenTelemetry.
type TraceContext struct {
	TraceID string
	SpanID  string
	Sampled bool
}

func (trace TraceContext) Validate() error {
	if !validHexIdentifier(trace.TraceID, 32) || !validHexIdentifier(trace.SpanID, 16) {
		return invalidArgument(ErrInvalidTraceContext, "invalid trace context", "invalid_trace_context", "observability.TraceContext.Validate", nil)
	}
	return nil
}

func (trace TraceContext) IsZero() bool { return trace.TraceID == "" && trace.SpanID == "" }

func (trace TraceContext) Attributes() Attributes {
	if trace.Validate() != nil {
		return Attributes{}
	}
	attributes, _ := NewAttributes(faults.Fields{
		"trace.id":      strings.ToLower(trace.TraceID),
		"span.id":       strings.ToLower(trace.SpanID),
		"trace.sampled": trace.Sampled,
	})
	return attributes
}

// TraceContextProvider extracts provider-owned trace correlation from ctx.
type TraceContextProvider interface {
	TraceContext(context.Context) (TraceContext, bool)
}

type TraceContextProviderFunc func(context.Context) (TraceContext, bool)

func (function TraceContextProviderFunc) TraceContext(ctx context.Context) (TraceContext, bool) {
	if function == nil {
		return TraceContext{}, false
	}
	return function(ctx)
}

type noopTraceContextProvider struct{}

func (noopTraceContextProvider) TraceContext(context.Context) (TraceContext, bool) {
	return TraceContext{}, false
}

func validHexIdentifier(value string, length int) bool {
	if len(value) != length || strings.Trim(value, "0") == "" {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
