// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package observability

import (
	"context"
	"errors"
	"testing"
	"time"

	"mindclade.internal/libs/go/faults"
	"mindclade.internal/libs/go/requestmeta"
)

type propagationTraceKey struct{}

type testTracePropagator struct {
	panicInject  bool
	panicExtract bool
	nilExtract   bool
}

func (propagator testTracePropagator) Inject(ctx context.Context, carrier requestmeta.TextMapCarrier) {
	if propagator.panicInject {
		panic("inject")
	}
	if trace, ok := ctx.Value(propagationTraceKey{}).(TraceContext); ok {
		carrier.Set("trace-id", trace.TraceID)
		carrier.Set("span-id", trace.SpanID)
	}
}

func (propagator testTracePropagator) Extract(ctx context.Context, carrier requestmeta.TextMapCarrier) context.Context {
	if propagator.panicExtract {
		panic("extract")
	}
	if propagator.nilExtract {
		return nil
	}
	trace := TraceContext{TraceID: carrier.Get("trace-id"), SpanID: carrier.Get("span-id")}
	if trace.Validate() == nil {
		return context.WithValue(ctx, propagationTraceKey{}, trace)
	}
	return ctx
}

func TestPropagatorExtractAndInject(t *testing.T) {
	requestID, err := requestmeta.NewRequestIDAt(time.UnixMilli(1_700_000_000_000))
	if err != nil {
		t.Fatal(err)
	}
	trace := TraceContext{TraceID: "4bf92f3577b34da6a3ce929d0e0e4736", SpanID: "00f067aa0ba902b7"}
	carrier := requestmeta.MapCarrier{
		requestmeta.PropagationKeyRequestID: requestID.String(),
		"trace-id":                          trace.TraceID,
		"span-id":                           trace.SpanID,
	}
	propagator := NewPropagator(testTracePropagator{})
	ctx, extractedID, err := propagator.Extract(context.Background(), carrier)
	if err != nil {
		t.Fatal(err)
	}
	if extractedID != requestID {
		t.Fatalf("extractedID = %v, want %v", extractedID, requestID)
	}
	if got, ok := ctx.Value(propagationTraceKey{}).(TraceContext); !ok || got.TraceID != trace.TraceID {
		t.Fatalf("trace not extracted: %+v, %v", got, ok)
	}

	outbound := requestmeta.MapCarrier{}
	ctx = context.WithValue(ctx, propagationTraceKey{}, trace)
	if err := propagator.Inject(ctx, outbound); err != nil {
		t.Fatal(err)
	}
	if outbound.Get(requestmeta.PropagationKeyRequestID) != requestID.String() || outbound.Get("trace-id") != trace.TraceID {
		t.Fatalf("outbound carrier = %#v", outbound)
	}
}

func TestPropagatorGeneratesRequestID(t *testing.T) {
	ctx, requestID, err := NewPropagator(nil).Extract(context.Background(), requestmeta.MapCarrier{})
	if err != nil {
		t.Fatal(err)
	}
	if requestID.IsZero() {
		t.Fatal("request ID was not generated")
	}
	if stored, ok := requestmeta.RequestIDFromContext(ctx); !ok || stored != requestID {
		t.Fatalf("context request ID = %v, %v", stored, ok)
	}
}

func TestPropagatorFailures(t *testing.T) {
	if _, _, err := NewPropagator(nil).Extract(nil, requestmeta.MapCarrier{}); !errors.Is(err, ErrNilContext) {
		t.Fatalf("Extract(nil) error = %v", err)
	}
	var nilCarrier requestmeta.MapCarrier
	if err := NewPropagator(nil).Inject(context.Background(), nilCarrier); !errors.Is(err, ErrNilCarrier) {
		t.Fatalf("Inject(nil carrier) error = %v", err)
	}
	for name, trace := range map[string]TracePropagator{
		"panic_extract": testTracePropagator{panicExtract: true},
		"nil_extract":   testTracePropagator{nilExtract: true},
	} {
		t.Run(name, func(t *testing.T) {
			_, _, err := NewPropagator(trace).Extract(context.Background(), requestmeta.MapCarrier{})
			if faults.CodeOf(err) != faults.CodeInternal || !errors.Is(err, ErrProviderPanic) {
				t.Fatalf("error = %v, code = %s", err, faults.CodeOf(err))
			}
		})
	}
	if err := NewPropagator(testTracePropagator{panicInject: true}).Inject(context.Background(), requestmeta.MapCarrier{}); faults.CodeOf(err) != faults.CodeInternal {
		t.Fatalf("panic inject error = %v", err)
	}
}
