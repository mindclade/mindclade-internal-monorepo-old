// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package observability

import (
	"context"
	"log/slog"

	"mindclade.internal/libs/go/clock"
	"mindclade.internal/libs/go/requestmeta"
)

type runtimeConfiguration struct {
	handler         slog.Handler
	static          Attributes
	traceContext    TraceContextProvider
	tracePropagator TracePropagator
	metricSink      MetricSink
	clock           clock.Clock
	errorHandler    ErrorHandler
	pipeline        *Pipeline
}

func defaultRuntimeConfiguration() runtimeConfiguration {
	return runtimeConfiguration{
		handler:         DiscardLogger().Handler(),
		traceContext:    noopTraceContextProvider{},
		tracePropagator: noopTracePropagator{},
		metricSink:      nopMetricSink{},
		clock:           clock.RealClock{},
		errorHandler:    nopErrorHandler{},
		pipeline:        &Pipeline{},
	}
}

// RuntimeOption configures Runtime.
type RuntimeOption func(*runtimeConfiguration) error

func WithSlogHandler(handler slog.Handler) RuntimeOption {
	return func(configuration *runtimeConfiguration) error {
		if nilInterface(handler) {
			return invalidArgument(ErrNilHandler, "slog handler must not be nil", "nil_handler", operationNewRuntime, nil)
		}
		configuration.handler = handler
		return nil
	}
}

func WithRuntimeAttributes(attributes Attributes) RuntimeOption {
	return func(configuration *runtimeConfiguration) error {
		configuration.static = attributes
		return nil
	}
}

func WithTraceContextProvider(provider TraceContextProvider) RuntimeOption {
	return func(configuration *runtimeConfiguration) error {
		if nilInterface(provider) {
			provider = noopTraceContextProvider{}
		}
		configuration.traceContext = provider
		return nil
	}
}

func WithTracePropagator(propagator TracePropagator) RuntimeOption {
	return func(configuration *runtimeConfiguration) error {
		if nilInterface(propagator) {
			propagator = noopTracePropagator{}
		}
		configuration.tracePropagator = propagator
		return nil
	}
}

func WithMetricSink(sink MetricSink) RuntimeOption {
	return func(configuration *runtimeConfiguration) error {
		if nilInterface(sink) {
			sink = nopMetricSink{}
		}
		configuration.metricSink = sink
		return nil
	}
}

func WithClock(value clock.Clock) RuntimeOption {
	return func(configuration *runtimeConfiguration) error {
		if nilInterface(value) {
			return invalidArgument(ErrInvalidMetric, "observability clock must not be nil", "nil_clock", operationNewRuntime, nil)
		}
		configuration.clock = value
		return nil
	}
}

func WithErrorHandler(handler ErrorHandler) RuntimeOption {
	return func(configuration *runtimeConfiguration) error {
		if nilInterface(handler) {
			handler = nopErrorHandler{}
		}
		configuration.errorHandler = handler
		return nil
	}
}

func WithLifecyclePipeline(pipeline *Pipeline) RuntimeOption {
	return func(configuration *runtimeConfiguration) error {
		if pipeline == nil {
			pipeline = &Pipeline{}
		}
		configuration.pipeline = pipeline
		return nil
	}
}

// Runtime is the process-local observability composition root. It owns no
// process-wide globals and is safe for concurrent use.
type Runtime struct {
	resource   Resource
	logger     *slog.Logger
	metrics    *Metrics
	propagator Propagator
	pipeline   *Pipeline
}

func NewRuntime(resource Resource, options ...RuntimeOption) (*Runtime, error) {
	if err := resource.Validate(); err != nil {
		return nil, err
	}
	configuration := defaultRuntimeConfiguration()
	for _, option := range options {
		if option != nil {
			if err := option(&configuration); err != nil {
				return nil, err
			}
		}
	}
	logger, err := NewLogger(configuration.handler, resource, configuration.static, configuration.traceContext)
	if err != nil {
		return nil, err
	}
	metrics, err := NewMetrics(configuration.metricSink, configuration.clock, configuration.errorHandler)
	if err != nil {
		return nil, err
	}
	return &Runtime{
		resource:   resource,
		logger:     logger,
		metrics:    metrics,
		propagator: NewPropagator(configuration.tracePropagator),
		pipeline:   configuration.pipeline,
	}, nil
}

func (runtime *Runtime) Resource() Resource {
	if runtime == nil {
		return Resource{}
	}
	return runtime.resource
}

func (runtime *Runtime) Logger() *slog.Logger {
	if runtime == nil || runtime.logger == nil {
		return DiscardLogger()
	}
	return runtime.logger
}

func (runtime *Runtime) Metrics() *Metrics {
	if runtime == nil {
		return nil
	}
	return runtime.metrics
}

func (runtime *Runtime) Pipeline() *Pipeline {
	if runtime == nil {
		return nil
	}
	return runtime.pipeline
}

// Context stores the runtime logger in ctx.
func (runtime *Runtime) Context(ctx context.Context) (context.Context, error) {
	if runtime == nil {
		return nil, invalidArgument(ErrNilRuntime, "observability runtime must not be nil", "nil_runtime", "observability.Runtime.Context", nil)
	}
	return ContextWithLogger(ctx, runtime.Logger())
}

func (runtime *Runtime) Extract(ctx context.Context, carrier requestmeta.TextMapCarrier) (context.Context, requestmeta.RequestID, error) {
	if runtime == nil {
		return nil, requestmeta.RequestID{}, invalidArgument(ErrNilRuntime, "observability runtime must not be nil", "nil_runtime", operationExtract, nil)
	}
	return runtime.propagator.Extract(ctx, carrier)
}

func (runtime *Runtime) Inject(ctx context.Context, carrier requestmeta.TextMapCarrier) error {
	if runtime == nil {
		return invalidArgument(ErrNilRuntime, "observability runtime must not be nil", "nil_runtime", operationInject, nil)
	}
	return runtime.propagator.Inject(ctx, carrier)
}

func (runtime *Runtime) ForceFlush(ctx context.Context) error {
	if runtime == nil {
		return invalidArgument(ErrNilRuntime, "observability runtime must not be nil", "nil_runtime", operationPipelineFlush, nil)
	}
	return runtime.pipeline.ForceFlush(ctx)
}

func (runtime *Runtime) Shutdown(ctx context.Context) error {
	if runtime == nil {
		return invalidArgument(ErrNilRuntime, "observability runtime must not be nil", "nil_runtime", operationPipelineClose, nil)
	}
	return runtime.pipeline.Shutdown(ctx)
}
