// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

package controller

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"

	"mindclade.internal/libs/go/faults"
	"mindclade.internal/libs/go/servicekit"
)

// Manager is the minimal controller-runtime manager contract needed by the
// process lifecycle. controller-runtime's manager.Manager satisfies it.
type Manager interface {
	Start(context.Context) error
}

// ManagerRuntime adapts a controller-runtime manager to servicekit while
// preserving a single owner for cancellation, readiness, and terminal errors.
type ManagerRuntime struct {
	manager   Manager
	readiness servicekit.Probe
	started   atomic.Bool
	running   atomic.Bool

	mu          sync.RWMutex
	terminalErr error
}

// NewManagerRuntime constructs the canonical Kubernetes-manager adapter.
// readiness may be nil when manager startup itself is the only readiness gate;
// production operators should normally provide a cache-sync or webhook-ready
// probe owned by their composition root.
func NewManagerRuntime(manager Manager, readiness servicekit.Probe) (*ManagerRuntime, error) {
	if isNil(manager) {
		return nil, faults.New(
			faults.CodeInvalidArgument,
			"Kubernetes manager is required",
			faults.WithReason("nil_kubernetes_manager"),
			faults.WithOperation("kubernetes.controller.NewManagerRuntime"),
			faults.WithRetryPolicy(faults.NoRetry()),
		)
	}
	return &ManagerRuntime{manager: manager, readiness: readiness}, nil
}

func (runtime *ManagerRuntime) run(ctx context.Context) error {
	if runtime == nil || isNil(runtime.manager) {
		return faults.New(
			faults.CodeFailedPrecondition,
			"Kubernetes manager is not configured",
			faults.WithReason("kubernetes_manager_not_configured"),
			faults.WithOperation("kubernetes.controller.ManagerRuntime.run"),
			faults.WithRetryPolicy(faults.NoRetry()),
		)
	}
	if !runtime.running.CompareAndSwap(false, true) {
		return faults.New(
			faults.CodeFailedPrecondition,
			"Kubernetes manager has already run",
			faults.WithReason("kubernetes_manager_already_run"),
			faults.WithOperation("kubernetes.controller.ManagerRuntime.run"),
			faults.WithRetryPolicy(faults.NoRetry()),
		)
	}
	runtime.started.Store(true)
	err := runtime.manager.Start(ctx)
	if ctx.Err() != nil && (errors.Is(err, context.Canceled) || err == nil) {
		err = nil
	}
	runtime.mu.Lock()
	runtime.terminalErr = err
	runtime.mu.Unlock()
	return err
}

// Component returns the only supported servicekit lifecycle adapter for a
// controller-runtime manager. The manager starts in the work stage and stops
// through cancellation of the service-owned context.
func (runtime *ManagerRuntime) Component(name string) servicekit.Component {
	return servicekit.Component{
		Name: name,
		Run:  runtime.run,
		Liveness: func(context.Context) error {
			if runtime == nil {
				return faults.New(faults.CodeFailedPrecondition, "Kubernetes manager is not configured", faults.WithReason("kubernetes_manager_not_configured"))
			}
			runtime.mu.RLock()
			err := runtime.terminalErr
			runtime.mu.RUnlock()
			return err
		},
		Readiness: func(ctx context.Context) error {
			if runtime == nil || !runtime.started.Load() {
				return faults.New(
					faults.CodeUnavailable,
					"Kubernetes manager is not ready",
					faults.WithReason("kubernetes_manager_not_started"),
					faults.WithRetryPolicy(faults.ImmediateRetry(0)),
				)
			}
			if runtime.readiness != nil {
				return runtime.readiness(ctx)
			}
			return nil
		},
	}
}
