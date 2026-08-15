// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package observability

import (
	"context"
	"errors"
	"fmt"
	"runtime/debug"
	"sort"
	"sync"

	"mindclade.internal/libs/go/faults"
)

// LifecycleHook flushes or shuts down one telemetry provider.
type LifecycleHook func(context.Context) error

// LifecycleComponent is one named telemetry provider lifecycle.
type LifecycleComponent struct {
	Name       string
	ForceFlush LifecycleHook
	Shutdown   LifecycleHook
}

func (component LifecycleComponent) validate() error {
	if !validName(component.Name, 128) {
		return invalidArgument(ErrInvalidComponent, "invalid telemetry lifecycle component name", "invalid_lifecycle_component_name", operationPipelineAdd, faults.Fields{"component_name": component.Name})
	}
	if component.ForceFlush == nil && component.Shutdown == nil {
		return invalidArgument(ErrInvalidComponent, "telemetry lifecycle component has no hooks", "empty_lifecycle_component", operationPipelineAdd, faults.Fields{"component_name": component.Name})
	}
	return nil
}

// Pipeline coordinates best-effort telemetry flush and shutdown. Its zero
// value is ready for use.
type Pipeline struct {
	mu         sync.Mutex
	components []LifecycleComponent
	names      map[string]struct{}
	shutting   bool
	closed     bool
	done       chan struct{}
	result     error
}

func NewPipeline(components ...LifecycleComponent) (*Pipeline, error) {
	pipeline := &Pipeline{}
	for _, component := range components {
		if err := pipeline.Add(component); err != nil {
			return nil, err
		}
	}
	return pipeline, nil
}

func (pipeline *Pipeline) ensureLocked() {
	if pipeline.names == nil {
		pipeline.names = make(map[string]struct{})
	}
	if pipeline.done == nil {
		pipeline.done = make(chan struct{})
	}
}

func (pipeline *Pipeline) Add(component LifecycleComponent) error {
	if pipeline == nil {
		return invalidArgument(ErrInvalidComponent, "telemetry lifecycle pipeline must not be nil", "nil_pipeline", operationPipelineAdd, nil)
	}
	if err := component.validate(); err != nil {
		return err
	}
	pipeline.mu.Lock()
	defer pipeline.mu.Unlock()
	pipeline.ensureLocked()
	if pipeline.shutting || pipeline.closed {
		return newFault(nil, ErrPipelineClosed, faults.CodeFailedPrecondition, "telemetry lifecycle pipeline is closed", "pipeline_closed", operationPipelineAdd, nil)
	}
	if _, exists := pipeline.names[component.Name]; exists {
		return newFault(nil, ErrDuplicateComponent, faults.CodeAlreadyExists, "telemetry lifecycle component is already registered", "duplicate_lifecycle_component", operationPipelineAdd, faults.Fields{"component_name": component.Name})
	}
	pipeline.components = append(pipeline.components, component)
	pipeline.names[component.Name] = struct{}{}
	return nil
}

func (pipeline *Pipeline) Components() []string {
	if pipeline == nil {
		return nil
	}
	pipeline.mu.Lock()
	names := make([]string, len(pipeline.components))
	for index, component := range pipeline.components {
		names[index] = component.Name
	}
	pipeline.mu.Unlock()
	return names
}

func (pipeline *Pipeline) ForceFlush(ctx context.Context) error {
	if ctx == nil {
		return invalidArgument(ErrNilContext, "nil telemetry flush context", "nil_context", operationPipelineFlush, nil)
	}
	if pipeline == nil {
		return invalidArgument(ErrInvalidComponent, "telemetry lifecycle pipeline must not be nil", "nil_pipeline", operationPipelineFlush, nil)
	}
	pipeline.mu.Lock()
	pipeline.ensureLocked()
	if pipeline.shutting || pipeline.closed {
		pipeline.mu.Unlock()
		return newFault(ctx, ErrPipelineClosed, faults.CodeFailedPrecondition, "telemetry lifecycle pipeline is closed", "pipeline_closed", operationPipelineFlush, nil)
	}
	components := append([]LifecycleComponent(nil), pipeline.components...)
	pipeline.mu.Unlock()

	var failures []error
	for _, component := range components {
		if ctx.Err() != nil {
			failures = append(failures, lifecycleContextError(ctx, operationPipelineFlush))
			break
		}
		if component.ForceFlush == nil {
			continue
		}
		if err := invokeLifecycle(ctx, component.Name, "flush", component.ForceFlush); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

// Shutdown invokes all shutdown hooks in reverse registration order. The first
// caller performs shutdown; concurrent callers wait for the same result while
// honoring their own contexts.
func (pipeline *Pipeline) Shutdown(ctx context.Context) error {
	if ctx == nil {
		return invalidArgument(ErrNilContext, "nil telemetry shutdown context", "nil_context", operationPipelineClose, nil)
	}
	if pipeline == nil {
		return invalidArgument(ErrInvalidComponent, "telemetry lifecycle pipeline must not be nil", "nil_pipeline", operationPipelineClose, nil)
	}

	pipeline.mu.Lock()
	pipeline.ensureLocked()
	if pipeline.closed {
		result := pipeline.result
		pipeline.mu.Unlock()
		return result
	}
	if pipeline.shutting {
		done := pipeline.done
		pipeline.mu.Unlock()
		select {
		case <-done:
			pipeline.mu.Lock()
			result := pipeline.result
			pipeline.mu.Unlock()
			return result
		case <-ctx.Done():
			return lifecycleContextError(ctx, operationPipelineClose)
		}
	}
	pipeline.shutting = true
	components := append([]LifecycleComponent(nil), pipeline.components...)
	done := pipeline.done
	pipeline.mu.Unlock()

	var failures []error
	for index := len(components) - 1; index >= 0; index-- {
		if ctx.Err() != nil {
			failures = append(failures, lifecycleContextError(ctx, operationPipelineClose))
			break
		}
		component := components[index]
		if component.Shutdown == nil {
			continue
		}
		if err := invokeLifecycle(ctx, component.Name, "shutdown", component.Shutdown); err != nil {
			failures = append(failures, err)
		}
	}
	result := errors.Join(failures...)

	pipeline.mu.Lock()
	pipeline.result = result
	pipeline.closed = true
	pipeline.shutting = false
	close(done)
	pipeline.mu.Unlock()
	return result
}

func invokeLifecycle(ctx context.Context, name, phase string, hook LifecycleHook) error {
	if hook == nil {
		return nil
	}
	result := make(chan error, 1)
	go func() {
		result <- callLifecycle(ctx, name, phase, hook)
	}()
	select {
	case err := <-result:
		return err
	case <-ctx.Done():
		return lifecycleContextError(ctx, "observability.Pipeline."+phase)
	}
}

func callLifecycle(ctx context.Context, name, phase string, hook LifecycleHook) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = newFault(
				ctx,
				errors.Join(ErrProviderPanic, fmt.Errorf("telemetry %s panic: %v", phase, recovered)),
				faults.CodeInternal,
				"telemetry provider lifecycle failed",
				"telemetry_provider_panicked",
				"observability.Pipeline."+phase,
				faults.Fields{"component_name": name, "phase": phase, "stack_bytes": len(debug.Stack())},
			)
		}
	}()
	if err := hook(ctx); err != nil {
		code := faults.CodeOf(err)
		if code == faults.CodeUnknown {
			code = faults.CodeUnavailable
		}
		return newFault(
			ctx,
			err,
			code,
			"telemetry provider lifecycle failed",
			"telemetry_provider_"+phase+"_failed",
			"observability.Pipeline."+phase,
			faults.Fields{"component_name": name, "phase": phase},
		)
	}
	return nil
}

func lifecycleContextError(ctx context.Context, operation string) error {
	cause := context.Cause(ctx)
	if cause == nil {
		cause = ctx.Err()
	}
	code := faults.CodeCanceled
	message := "telemetry lifecycle canceled"
	reason := "telemetry_lifecycle_canceled"
	if errors.Is(cause, context.DeadlineExceeded) {
		code = faults.CodeDeadlineExceeded
		message = "telemetry lifecycle deadline exceeded"
		reason = "telemetry_lifecycle_deadline_exceeded"
	}
	return newFault(ctx, cause, code, message, reason, operation, nil)
}

// SortedComponents returns a stable diagnostic view without exposing
// registration order. Components() should be used when order matters.
func (pipeline *Pipeline) SortedComponents() []string {
	names := pipeline.Components()
	sort.Strings(names)
	return names
}
