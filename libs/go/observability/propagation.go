// Copyright 2026 Mindclade. All rights reserved.
// Confidential and proprietary.

package observability

import (
	"context"
	"errors"
	"fmt"
	"runtime/debug"

	"mindclade.internal/libs/go/faults"
	"mindclade.internal/libs/go/requestmeta"
)

// TracePropagator adapts provider-owned distributed trace propagation. It
// intentionally mirrors the no-error shape used by OpenTelemetry propagators.
type TracePropagator interface {
	Inject(context.Context, requestmeta.TextMapCarrier)
	Extract(context.Context, requestmeta.TextMapCarrier) context.Context
}

type noopTracePropagator struct{}

func (noopTracePropagator) Inject(context.Context, requestmeta.TextMapCarrier) {}
func (noopTracePropagator) Extract(ctx context.Context, _ requestmeta.TextMapCarrier) context.Context {
	return ctx
}

// Propagator coordinates trace propagation with Mindclade request lineage.
type Propagator struct{ trace TracePropagator }

func NewPropagator(trace TracePropagator) Propagator {
	if nilInterface(trace) {
		trace = noopTracePropagator{}
	}
	return Propagator{trace: trace}
}

// Extract imports provider trace context and Mindclade request metadata, then
// guarantees a canonical request ID.
func (propagator Propagator) Extract(ctx context.Context, carrier requestmeta.TextMapCarrier) (result context.Context, requestID requestmeta.RequestID, err error) {
	if ctx == nil {
		return nil, requestmeta.RequestID{}, invalidArgument(ErrNilContext, "nil propagation context", "nil_context", operationExtract, nil)
	}
	if nilInterface(carrier) {
		return nil, requestmeta.RequestID{}, invalidArgument(ErrNilCarrier, "nil propagation carrier", "nil_carrier", operationExtract, nil)
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			err = newFault(ctx, errors.Join(ErrProviderPanic, fmt.Errorf("trace propagator panic: %v", recovered)), faults.CodeInternal, "trace propagation failed", "trace_propagator_panicked", operationExtract, faults.Fields{"stack_bytes": len(debug.Stack())})
			result = nil
			requestID = requestmeta.RequestID{}
		}
	}()
	trace := propagator.trace
	if nilInterface(trace) {
		trace = noopTracePropagator{}
	}
	result = trace.Extract(ctx, carrier)
	if result == nil {
		return nil, requestmeta.RequestID{}, newFault(ctx, ErrProviderPanic, faults.CodeInternal, "trace propagation failed", "trace_propagator_returned_nil_context", operationExtract, nil)
	}
	return requestmeta.ExtractOrGenerate(result, carrier)
}

// Inject exports Mindclade request metadata and provider trace context.
func (propagator Propagator) Inject(ctx context.Context, carrier requestmeta.TextMapCarrier) (err error) {
	if ctx == nil {
		return invalidArgument(ErrNilContext, "nil propagation context", "nil_context", operationInject, nil)
	}
	if nilInterface(carrier) {
		return invalidArgument(ErrNilCarrier, "nil propagation carrier", "nil_carrier", operationInject, nil)
	}
	if err := requestmeta.Inject(ctx, carrier); err != nil {
		return err
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			err = newFault(ctx, errors.Join(ErrProviderPanic, fmt.Errorf("trace propagator panic: %v", recovered)), faults.CodeInternal, "trace propagation failed", "trace_propagator_panicked", operationInject, faults.Fields{"stack_bytes": len(debug.Stack())})
		}
	}()
	trace := propagator.trace
	if nilInterface(trace) {
		trace = noopTracePropagator{}
	}
	trace.Inject(ctx, carrier)
	return nil
}
