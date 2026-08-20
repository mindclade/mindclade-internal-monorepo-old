// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package observability

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"go.mindclade.dev/libs/go/clock"
	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/requestmeta"
)

func TestRuntimeComposition(t *testing.T) {
	start := time.Date(2026, 8, 12, 17, 0, 0, 0, time.UTC)
	fakeClock := clock.NewFake(start)
	metrics := &measurementRecorder{}
	trace := testTracePropagator{}
	pipeline := &Pipeline{}
	var output bytes.Buffer
	resource, err := NewResource("runtime-gateway")
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := NewRuntime(
		resource,
		WithSlogHandler(slog.NewJSONHandler(&output, nil)),
		WithRuntimeAttributes(MustAttributes(faults.Fields{"deployment.ring": "dev"})),
		WithTracePropagator(trace),
		WithMetricSink(metrics),
		WithClock(fakeClock),
		WithLifecyclePipeline(pipeline),
	)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.Resource().ServiceName() != "runtime-gateway" || runtime.Logger() == nil || runtime.Metrics() == nil || runtime.Pipeline() != pipeline {
		t.Fatalf("runtime accessors invalid: %+v", runtime)
	}
	ctx, err := runtime.Context(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if LoggerFromContext(ctx, nil) != runtime.Logger() {
		t.Fatal("runtime logger not installed in context")
	}
	if err := runtime.Metrics().Gauge(ctx, "runtime.queue_depth", 3, "1", Labels{}); err != nil {
		t.Fatal(err)
	}
	if got := metrics.Records(); len(got) != 1 || !got[0].At.Equal(start) {
		t.Fatalf("metrics = %+v", got)
	}

	carrier := requestmeta.MapCarrier{}
	extracted, requestID, err := runtime.Extract(context.Background(), carrier)
	if err != nil || requestID.IsZero() {
		t.Fatalf("Extract() = %v, %v", requestID, err)
	}
	if err := runtime.Inject(extracted, carrier); err != nil {
		t.Fatal(err)
	}
	if carrier.Get(requestmeta.PropagationKeyRequestID) != requestID.String() {
		t.Fatalf("carrier = %#v", carrier)
	}
	if err := runtime.ForceFlush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := runtime.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestRuntimeOptionsAndNilContracts(t *testing.T) {
	resource, _ := NewResource("service")
	if _, err := NewRuntime(resource, WithSlogHandler(nil)); !errors.Is(err, ErrNilHandler) {
		t.Fatalf("WithSlogHandler(nil) error = %v", err)
	}
	if _, err := NewRuntime(resource, WithClock(nil)); err == nil {
		t.Fatal("WithClock(nil) succeeded")
	}
	var runtime *Runtime
	if _, err := runtime.Context(context.Background()); !errors.Is(err, ErrNilRuntime) {
		t.Fatalf("nil Runtime.Context error = %v", err)
	}
	if err := runtime.Inject(context.Background(), requestmeta.MapCarrier{}); !errors.Is(err, ErrNilRuntime) {
		t.Fatalf("nil Runtime.Inject error = %v", err)
	}
	if runtime.Logger() == nil || runtime.Resource().ServiceName() != "" || runtime.Metrics() != nil || runtime.Pipeline() != nil {
		t.Fatal("nil runtime accessors are not safe")
	}
}
