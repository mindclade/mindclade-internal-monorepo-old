// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

package observability

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"

	"mindclade.internal/libs/go/servicekit"
)

type recordHandler struct {
	mu      sync.Mutex
	records []slog.Record
}

func (handler *recordHandler) Enabled(context.Context, slog.Level) bool { return true }
func (handler *recordHandler) Handle(_ context.Context, record slog.Record) error {
	handler.mu.Lock()
	handler.records = append(handler.records, record.Clone())
	handler.mu.Unlock()
	return nil
}
func (handler *recordHandler) WithAttrs([]slog.Attr) slog.Handler { return handler }
func (handler *recordHandler) WithGroup(string) slog.Handler      { return handler }

type measurementSink struct {
	mu     sync.Mutex
	values []Measurement
}

func (sink *measurementSink) Record(_ context.Context, value Measurement) {
	sink.mu.Lock()
	sink.values = append(sink.values, value)
	sink.mu.Unlock()
}

func TestServiceObserverEmitsLogsAndMetrics(t *testing.T) {
	handler := &recordHandler{}
	sink := &measurementSink{}
	resource, err := NewResource("control-plane")
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := NewRuntime(resource, WithSlogHandler(handler), WithMetricSink(sink))
	if err != nil {
		t.Fatal(err)
	}
	observer := NewServiceObserver(runtime)
	observer.Observe(servicekit.Event{Kind: servicekit.EventComponentStarted, Service: "control-plane", Component: "postgres", At: time.Now(), Duration: time.Millisecond})
	handler.mu.Lock()
	logCount := len(handler.records)
	handler.mu.Unlock()
	if logCount != 1 {
		t.Fatalf("logs=%d", logCount)
	}
	sink.mu.Lock()
	metricCount := len(sink.values)
	sink.mu.Unlock()
	if metricCount < 2 {
		t.Fatalf("metrics=%d", metricCount)
	}
}

func TestRuntimeServiceComponentFlushesAndShutsDown(t *testing.T) {
	var mu sync.Mutex
	var order []string
	pipeline, err := NewPipeline(LifecycleComponent{
		Name:       "provider",
		ForceFlush: func(context.Context) error { mu.Lock(); order = append(order, "flush"); mu.Unlock(); return nil },
		Shutdown:   func(context.Context) error { mu.Lock(); order = append(order, "shutdown"); mu.Unlock(); return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	resource, err := NewResource("registry")
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := NewRuntime(resource, WithLifecyclePipeline(pipeline))
	if err != nil {
		t.Fatal(err)
	}
	component := runtime.ServiceComponent("telemetry")
	if err := component.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := component.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	got := append([]string(nil), order...)
	mu.Unlock()
	if len(got) != 2 || got[0] != "flush" || got[1] != "shutdown" {
		t.Fatalf("order=%v", got)
	}
}
