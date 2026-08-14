// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

package servicekit

import (
	"context"
	"os"
	"os/signal"

	"mindclade.internal/libs/go/faults"
)

// SignalContext returns a context canceled by the first configured operating
// system signal. When no non-nil signals are supplied, DefaultSignals is used.
func SignalContext(parent context.Context, signals ...os.Signal) (context.Context, context.CancelFunc, error) {
	if parent == nil {
		return nil, nil, nilContextError(operationSignalContext)
	}
	selected := make([]os.Signal, 0, len(signals))
	for _, configured := range signals {
		if configured != nil {
			selected = append(selected, configured)
		}
	}
	if len(selected) == 0 {
		selected = DefaultSignals()
	}
	parent = faults.ContextWithOperation(parent, operationSignalContext)
	ctx, stop := signal.NotifyContext(parent, selected...)
	return ctx, stop, nil
}

// RunWithSignals runs the service until a component returns, the parent is
// canceled, or one of the selected signals arrives.
func (service *Service) RunWithSignals(parent context.Context, signals ...os.Signal) error {
	if service == nil {
		return nilServiceError("servicekit.Service.RunWithSignals")
	}
	ctx, stop, err := SignalContext(parent, signals...)
	if err != nil {
		return err
	}
	defer stop()
	return service.Run(ctx)
}
