// Copyright 2026 Mindclade. All rights reserved.
// Confidential and proprietary.

package observability

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"mindclade.internal/libs/go/clock"
	"mindclade.internal/libs/go/faults"
)

const (
	MaximumMetricNameLength        = 128
	MaximumMetricDescriptionLength = 512
	MaximumMetricUnitLength        = 64
)

type MetricKind string

const (
	MetricCounter       MetricKind = "counter"
	MetricUpDownCounter MetricKind = "up_down_counter"
	MetricHistogram     MetricKind = "histogram"
	MetricGauge         MetricKind = "gauge"
)

func (kind MetricKind) Valid() bool {
	switch kind {
	case MetricCounter, MetricUpDownCounter, MetricHistogram, MetricGauge:
		return true
	default:
		return false
	}
}

// Measurement is one provider-neutral metric observation.
type Measurement struct {
	Name        string
	Kind        MetricKind
	Value       float64
	Unit        string
	Description string
	Labels      Labels
	At          time.Time
}

func (measurement Measurement) Validate() error {
	switch {
	case !validMetricName(measurement.Name):
		return invalidArgument(ErrInvalidMetric, "invalid metric name", "invalid_metric_name", operationRecordMetric, faults.Fields{"metric_name": measurement.Name})
	case !measurement.Kind.Valid():
		return invalidArgument(ErrInvalidMetric, "invalid metric kind", "invalid_metric_kind", operationRecordMetric, faults.Fields{"metric_kind": string(measurement.Kind)})
	case math.IsNaN(measurement.Value) || math.IsInf(measurement.Value, 0):
		return invalidArgument(ErrInvalidMetric, "invalid metric value", "invalid_metric_value", operationRecordMetric, faults.Fields{"metric_name": measurement.Name})
	case measurement.Kind == MetricCounter && measurement.Value < 0:
		return invalidArgument(ErrInvalidMetric, "counter delta must not be negative", "negative_counter_delta", operationRecordMetric, faults.Fields{"metric_name": measurement.Name})
	case len(measurement.Unit) > MaximumMetricUnitLength || strings.TrimSpace(measurement.Unit) != measurement.Unit:
		return invalidArgument(ErrInvalidMetric, "invalid metric unit", "invalid_metric_unit", operationRecordMetric, faults.Fields{"metric_name": measurement.Name})
	case len(measurement.Description) > MaximumMetricDescriptionLength:
		return invalidArgument(ErrInvalidMetric, "metric description is too long", "invalid_metric_description", operationRecordMetric, faults.Fields{"metric_name": measurement.Name})
	}
	return nil
}

func (measurement Measurement) Fields() faults.Fields {
	fields := faults.Fields{
		"metric.name":  measurement.Name,
		"metric.kind":  string(measurement.Kind),
		"metric.value": measurement.Value,
	}
	if measurement.Unit != "" {
		fields["metric.unit"] = measurement.Unit
	}
	for key, value := range measurement.Labels.values {
		fields["metric.label."+key] = value
	}
	return fields.Clone()
}

// MetricSink consumes validated measurements. It should be concurrency-safe
// and non-blocking on hot paths.
type MetricSink interface {
	Record(context.Context, Measurement)
}

type MetricSinkFunc func(context.Context, Measurement)

func (function MetricSinkFunc) Record(ctx context.Context, measurement Measurement) {
	if function != nil {
		function(ctx, measurement)
	}
}

type nopMetricSink struct{}

func (nopMetricSink) Record(context.Context, Measurement) {}

func CombineMetricSinks(sinks ...MetricSink) MetricSink {
	captured := make([]MetricSink, 0, len(sinks))
	for _, sink := range sinks {
		if !nilInterface(sink) {
			captured = append(captured, sink)
		}
	}
	if len(captured) == 0 {
		return nopMetricSink{}
	}
	return MetricSinkFunc(func(ctx context.Context, measurement Measurement) {
		for _, sink := range captured {
			safeMetricRecord(ctx, sink, measurement, nopErrorHandler{})
		}
	})
}

// Metrics validates and timestamps measurements before sending them to a sink.
type Metrics struct {
	sink         MetricSink
	clock        clock.Clock
	errorHandler ErrorHandler
}

func NewMetrics(sink MetricSink, valueClock clock.Clock, errorHandler ErrorHandler) (*Metrics, error) {
	if nilInterface(sink) {
		sink = nopMetricSink{}
	}
	if nilInterface(valueClock) {
		return nil, invalidArgument(ErrInvalidMetric, "metric clock must not be nil", "nil_metric_clock", operationNewMetrics, nil)
	}
	if nilInterface(errorHandler) {
		errorHandler = nopErrorHandler{}
	}
	return &Metrics{sink: sink, clock: valueClock, errorHandler: errorHandler}, nil
}

func (metrics *Metrics) Record(ctx context.Context, measurement Measurement) error {
	if ctx == nil {
		return invalidArgument(ErrNilContext, "nil metric context", "nil_context", operationRecordMetric, nil)
	}
	if metrics == nil {
		return invalidArgument(ErrInvalidMetric, "metrics recorder must not be nil", "nil_metrics", operationRecordMetric, nil)
	}
	if measurement.At.IsZero() {
		measurement.At = metrics.clock.Now()
	}
	if err := measurement.Validate(); err != nil {
		return err
	}
	safeMetricRecord(ctx, metrics.sink, measurement, metrics.errorHandler)
	return nil
}

func (metrics *Metrics) Counter(ctx context.Context, name string, delta float64, labels Labels) error {
	return metrics.Record(ctx, Measurement{Name: name, Kind: MetricCounter, Value: delta, Labels: labels})
}
func (metrics *Metrics) UpDownCounter(ctx context.Context, name string, delta float64, labels Labels) error {
	return metrics.Record(ctx, Measurement{Name: name, Kind: MetricUpDownCounter, Value: delta, Labels: labels})
}
func (metrics *Metrics) Histogram(ctx context.Context, name string, value float64, unit string, labels Labels) error {
	return metrics.Record(ctx, Measurement{Name: name, Kind: MetricHistogram, Value: value, Unit: unit, Labels: labels})
}
func (metrics *Metrics) Gauge(ctx context.Context, name string, value float64, unit string, labels Labels) error {
	return metrics.Record(ctx, Measurement{Name: name, Kind: MetricGauge, Value: value, Unit: unit, Labels: labels})
}
func (metrics *Metrics) Duration(ctx context.Context, name string, duration time.Duration, labels Labels) error {
	return metrics.Histogram(ctx, name, duration.Seconds(), "s", labels)
}

func safeMetricRecord(ctx context.Context, sink MetricSink, measurement Measurement, handler ErrorHandler) {
	if nilInterface(sink) {
		return
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			safeHandle(handler, newFault(ctx, errors.Join(ErrProviderPanic, fmt.Errorf("metric sink panic: %v", recovered)), faults.CodeInternal, "metric recording failed", "metric_sink_panicked", operationRecordMetric, measurement.Fields()))
		}
	}()
	sink.Record(ctx, measurement)
}

func validMetricName(name string) bool {
	if name == "" || len(name) > MaximumMetricNameLength || name != strings.ToLower(name) || strings.TrimSpace(name) != name {
		return false
	}
	previousSeparator := false
	for index := 0; index < len(name); index++ {
		character := name[index]
		letter := character >= 'a' && character <= 'z'
		digit := character >= '0' && character <= '9'
		separator := character == '.' || character == '_'
		if !letter && !digit && !separator || index == 0 && !letter || index == len(name)-1 && separator || separator && previousSeparator {
			return false
		}
		previousSeparator = separator
	}
	return true
}
