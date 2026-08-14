// Copyright 2026 Mindclade. All rights reserved.
// Confidential and proprietary.

package observability

import (
	"context"
	"errors"
	"math"
	"sync"
	"testing"
	"time"

	"mindclade.internal/libs/go/clock"
	"mindclade.internal/libs/go/faults"
)

type measurementRecorder struct {
	mu      sync.Mutex
	records []Measurement
}

func (recorder *measurementRecorder) Record(_ context.Context, measurement Measurement) {
	recorder.mu.Lock()
	recorder.records = append(recorder.records, measurement)
	recorder.mu.Unlock()
}

func (recorder *measurementRecorder) Records() []Measurement {
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	return append([]Measurement(nil), recorder.records...)
}

func TestMetricsValidateTimestampAndRecord(t *testing.T) {
	start := time.Date(2026, 8, 12, 16, 0, 0, 0, time.UTC)
	fakeClock := clock.NewFake(start)
	recorder := &measurementRecorder{}
	metrics, err := NewMetrics(recorder, fakeClock, nil)
	if err != nil {
		t.Fatal(err)
	}
	labels := MustLabels(map[string]string{"component": "scheduler"})
	if err := metrics.Counter(context.Background(), "service.requests", 2, labels); err != nil {
		t.Fatal(err)
	}
	if err := metrics.Duration(context.Background(), "service.latency", 250*time.Millisecond, labels); err != nil {
		t.Fatal(err)
	}
	records := recorder.Records()
	if len(records) != 2 {
		t.Fatalf("record count = %d", len(records))
	}
	if !records[0].At.Equal(start) || records[0].Kind != MetricCounter || records[0].Value != 2 {
		t.Fatalf("counter = %+v", records[0])
	}
	if records[1].Kind != MetricHistogram || records[1].Value != 0.25 || records[1].Unit != "s" {
		t.Fatalf("duration = %+v", records[1])
	}
}

func TestMeasurementValidation(t *testing.T) {
	tests := []Measurement{
		{Name: "Bad.Name", Kind: MetricCounter, Value: 1},
		{Name: "metric.value", Kind: MetricKind("bad"), Value: 1},
		{Name: "metric.value", Kind: MetricCounter, Value: -1},
		{Name: "metric.value", Kind: MetricGauge, Value: math.Inf(1)},
		{Name: "metric.value", Kind: MetricGauge, Value: 1, Unit: " bad"},
	}
	for _, measurement := range tests {
		if err := measurement.Validate(); !errors.Is(err, ErrInvalidMetric) {
			t.Fatalf("Validate(%+v) error = %v", measurement, err)
		}
	}
	if _, err := NewMetrics(nil, nil, nil); !errors.Is(err, ErrInvalidMetric) {
		t.Fatalf("NewMetrics(nil clock) error = %v", err)
	}
}

func TestMetricSinkPanicIsReported(t *testing.T) {
	var mu sync.Mutex
	var reported []error
	metrics, err := NewMetrics(
		MetricSinkFunc(func(context.Context, Measurement) { panic("boom") }),
		clock.RealClock{},
		ErrorHandlerFunc(func(err error) {
			mu.Lock()
			reported = append(reported, err)
			mu.Unlock()
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := metrics.Gauge(context.Background(), "service.queue_depth", 4, "1", Labels{}); err != nil {
		t.Fatalf("Record returned provider failure: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(reported) != 1 || faults.CodeOf(reported[0]) != faults.CodeInternal || !errors.Is(reported[0], ErrProviderPanic) {
		t.Fatalf("reported = %#v", reported)
	}
}

func TestCombineMetricSinksIsolatesPanics(t *testing.T) {
	recorder := &measurementRecorder{}
	combined := CombineMetricSinks(
		MetricSinkFunc(func(context.Context, Measurement) { panic("ignored") }),
		recorder,
	)
	combined.Record(context.Background(), Measurement{Name: "service.count", Kind: MetricCounter, Value: 1, At: time.Now()})
	if len(recorder.Records()) != 1 {
		t.Fatal("healthy sink was not invoked after panic")
	}
}
