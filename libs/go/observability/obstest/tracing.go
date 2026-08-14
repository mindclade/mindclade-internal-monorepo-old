// Copyright 2026 Mindclade. All rights reserved.
// Confidential and proprietary.

package obstest

import (
	"context"
	"sync"

	"mindclade.internal/libs/go/observability"
	"mindclade.internal/libs/go/requestmeta"
)

type traceContextKey struct{}

// WithTraceContext stores trace context for StaticTraceProvider.
func WithTraceContext(ctx context.Context, trace observability.TraceContext) context.Context {
	return context.WithValue(ctx, traceContextKey{}, trace)
}

// StaticTraceProvider reads trace context installed by WithTraceContext.
type StaticTraceProvider struct{}

func (StaticTraceProvider) TraceContext(ctx context.Context) (observability.TraceContext, bool) {
	if ctx == nil {
		return observability.TraceContext{}, false
	}
	trace, ok := ctx.Value(traceContextKey{}).(observability.TraceContext)
	return trace, ok && trace.Validate() == nil
}

// TracePropagator is a deterministic test propagator using fixed headers.
type TracePropagator struct {
	mu       sync.Mutex
	Injects  int
	Extracts int
}

func (propagator *TracePropagator) Inject(ctx context.Context, carrier requestmeta.TextMapCarrier) {
	if propagator == nil {
		return
	}
	propagator.mu.Lock()
	propagator.Injects++
	propagator.mu.Unlock()
	if trace, ok := (StaticTraceProvider{}).TraceContext(ctx); ok {
		carrier.Set("test-trace-id", trace.TraceID)
		carrier.Set("test-span-id", trace.SpanID)
	}
}

func (propagator *TracePropagator) Extract(ctx context.Context, carrier requestmeta.TextMapCarrier) context.Context {
	if propagator == nil {
		return ctx
	}
	propagator.mu.Lock()
	propagator.Extracts++
	propagator.mu.Unlock()
	trace := observability.TraceContext{TraceID: carrier.Get("test-trace-id"), SpanID: carrier.Get("test-span-id")}
	if trace.Validate() != nil {
		return ctx
	}
	return WithTraceContext(ctx, trace)
}

func (propagator *TracePropagator) Counts() (injects, extracts int) {
	if propagator == nil {
		return 0, 0
	}
	propagator.mu.Lock()
	injects, extracts = propagator.Injects, propagator.Extracts
	propagator.mu.Unlock()
	return injects, extracts
}
