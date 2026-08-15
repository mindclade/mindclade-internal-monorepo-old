// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package observability

import (
	"context"
	"errors"
	"log/slog"

	"mindclade.internal/libs/go/faults"
	"mindclade.internal/libs/go/servicekit"
)

// ServiceObserver adapts servicekit lifecycle events to the process-local
// observability runtime. It emits bounded structured logs and stable metrics.
// The adapter is intentionally synchronous; configured sinks must satisfy the
// non-blocking contracts already required by Runtime.
type ServiceObserver struct{ runtime *Runtime }

// NewServiceObserver creates the canonical lifecycle observer. A nil runtime
// returns nil so composition roots can pass the result directly to
// servicekit.WithObserver during explicitly uninstrumented tests.
func NewServiceObserver(runtime *Runtime) servicekit.Observer {
	if runtime == nil {
		return nil
	}
	return ServiceObserver{runtime: runtime}
}

func (observer ServiceObserver) Observe(event servicekit.Event) {
	if observer.runtime == nil {
		return
	}
	ctx := context.Background()
	fields := event.Fields()
	attributes := FieldsAttrs(fields)
	level := slog.LevelDebug
	switch event.Kind {
	case servicekit.EventStateChanged:
		level = slog.LevelInfo
	case servicekit.EventComponentStarted, servicekit.EventComponentStopped:
		level = slog.LevelInfo
	}
	message := "service lifecycle event"
	if event.Err != nil {
		level = slog.LevelError
		LogError(ctx, observer.runtime.Logger(), level, message, event.Err, attributes...)
	} else {
		observer.runtime.Logger().LogAttrs(ctx, level, message, attributes...)
	}
	observer.recordMetrics(ctx, event)
}

func (observer ServiceObserver) recordMetrics(ctx context.Context, event servicekit.Event) {
	metrics := observer.runtime.Metrics()
	if metrics == nil {
		return
	}
	labelsMap := map[string]string{
		"service": event.Service,
		"event":   string(event.Kind),
	}
	if event.Component != "" {
		labelsMap["component"] = event.Component
	}
	if code := event.ErrorCode(); code != faults.CodeUnknown {
		labelsMap["code"] = code.String()
	}
	labels, err := NewLabels(labelsMap)
	if err != nil {
		return
	}
	_ = metrics.Counter(ctx, "servicekit.lifecycle.events", 1, labels)
	if event.Duration > 0 {
		_ = metrics.Duration(ctx, "servicekit.lifecycle.duration", event.Duration, labels)
	}
	if event.Kind == servicekit.EventStateChanged {
		stateLabels, stateErr := NewLabels(map[string]string{"service": event.Service, "state": event.To.String()})
		if stateErr == nil {
			_ = metrics.Gauge(ctx, "servicekit.lifecycle.state", 1, "1", stateLabels)
		}
	}
}

// ServiceComponent adapts Runtime lifecycle to servicekit. Register this in
// StageFoundation so telemetry starts before every other component and shuts
// down last. Stop first force-flushes and then shuts down providers within the
// servicekit-owned shutdown budget.
func (runtime *Runtime) ServiceComponent(name string) servicekit.Component {
	return servicekit.Component{
		Name: name,
		Start: func(context.Context) error {
			if runtime == nil || runtime.pipeline == nil || runtime.logger == nil || runtime.metrics == nil {
				return invalidArgument(ErrNilRuntime, "observability runtime is not configured", "nil_runtime", "observability.Runtime.ServiceComponent", nil)
			}
			return nil
		},
		Stop: func(ctx context.Context) error {
			if runtime == nil {
				return nil
			}
			flushErr := runtime.ForceFlush(ctx)
			shutdownErr := runtime.Shutdown(ctx)
			return errors.Join(flushErr, shutdownErr)
		},
		Liveness: func(context.Context) error {
			if runtime == nil || runtime.pipeline == nil {
				return invalidArgument(ErrNilRuntime, "observability runtime is not configured", "nil_runtime", "observability.Runtime.ServiceComponent.Liveness", nil)
			}
			return nil
		},
		Readiness: func(context.Context) error {
			if runtime == nil || runtime.pipeline == nil {
				return invalidArgument(ErrNilRuntime, "observability runtime is not configured", "nil_runtime", "observability.Runtime.ServiceComponent.Readiness", nil)
			}
			return nil
		},
	}
}
