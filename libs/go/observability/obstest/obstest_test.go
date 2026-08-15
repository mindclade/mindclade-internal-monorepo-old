// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package obstest

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"mindclade.internal/libs/go/observability"
	"mindclade.internal/libs/go/requestmeta"
)

func TestCaptureHandlerAndDefensiveCopies(t *testing.T) {
	handler := NewCaptureHandler(slog.LevelInfo)
	logger := slog.New(handler.WithGroup("rpc").WithAttrs([]slog.Attr{slog.String("service", "runs")}))
	logger.Info("handled", slog.Group("result", slog.String("code", "ok")))
	records := handler.Records()
	if len(records) != 1 || records[0].Attributes["rpc.service"] != "runs" || records[0].Attributes["rpc.result.code"] != "ok" {
		t.Fatalf("records = %#v", records)
	}
	records[0].Attributes["rpc.service"] = "mutated"
	if handler.Records()[0].Attributes["rpc.service"] != "runs" {
		t.Fatal("captured record mutated through returned value")
	}
	handler.Reset()
	if len(handler.Records()) != 0 {
		t.Fatal("Reset did not clear records")
	}
}

func TestMetricAndErrorRecorders(t *testing.T) {
	labels := observability.MustLabels(map[string]string{"component": "worker"})
	metrics := &MetricRecorder{}
	metrics.Record(context.Background(), observability.Measurement{Name: "service.count", Kind: observability.MetricCounter, Value: 1, Labels: labels, At: time.Now()})
	records := metrics.Measurements()
	if len(records) != 1 || records[0].Labels.Map()["component"] != "worker" {
		t.Fatalf("records = %#v", records)
	}
	labelsCopy := records[0].Labels.Map()
	labelsCopy["component"] = "mutated"
	if metrics.Measurements()[0].Labels.Map()["component"] != "worker" {
		t.Fatal("metric labels mutated through returned value")
	}

	errorsRecorder := &ErrorRecorder{}
	failure := errors.New("failure")
	errorsRecorder.Handle(failure)
	got := errorsRecorder.Errors()
	if len(got) != 1 || got[0] != failure {
		t.Fatalf("errors = %#v", got)
	}
	errorsRecorder.Reset()
	if len(errorsRecorder.Errors()) != 0 {
		t.Fatal("error reset failed")
	}
}

func TestStaticTraceProviderAndPropagator(t *testing.T) {
	trace := observability.TraceContext{TraceID: "4bf92f3577b34da6a3ce929d0e0e4736", SpanID: "00f067aa0ba902b7"}
	ctx := WithTraceContext(context.Background(), trace)
	if got, ok := (StaticTraceProvider{}).TraceContext(ctx); !ok || got != trace {
		t.Fatalf("TraceContext() = %+v, %v", got, ok)
	}
	propagator := &TracePropagator{}
	carrier := requestmeta.MapCarrier{}
	propagator.Inject(ctx, carrier)
	extracted := propagator.Extract(context.Background(), carrier)
	if got, ok := (StaticTraceProvider{}).TraceContext(extracted); !ok || got.TraceID != trace.TraceID {
		t.Fatalf("extracted trace = %+v, %v", got, ok)
	}
	injects, extracts := propagator.Counts()
	if injects != 1 || extracts != 1 {
		t.Fatalf("counts = %d, %d", injects, extracts)
	}
}
