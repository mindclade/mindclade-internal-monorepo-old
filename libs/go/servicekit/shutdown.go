// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

package servicekit

import (
	"context"
	"errors"

	"mindclade.internal/libs/go/faults"
)

func (service *Service) newShutdownContext(parent context.Context) (context.Context, context.CancelFunc) {
	detached := context.WithoutCancel(parent)
	detached = faults.ContextWithOperation(detached, operationShutdown)
	if service.config.shutdownTimeout > 0 {
		return withClockTimeout(detached, service.config.clock, service.config.shutdownTimeout)
	}
	return context.WithCancel(detached)
}

func (service *Service) drainComponents(ctx context.Context, components []Component) error {
	failures := make([]error, 0)
	for index := len(components) - 1; index >= 0; index-- {
		component := components[index]
		if component.Drain == nil {
			continue
		}
		if err := ctx.Err(); err != nil {
			failures = append(failures, shutdownTimeoutError(ctx, service.name, err, service.config.shutdownTimeout))
			break
		}
		beganAt := service.config.clock.Now()
		service.emit(Event{Kind: EventComponentDraining, Service: service.name, Component: component.Name, At: beganAt})
		componentCtx := faults.ContextWithOperation(ctx, componentOperation(PhaseDrain))
		cancel := func() {}
		if service.config.componentDrainTimeout > 0 {
			componentCtx, cancel = withClockTimeout(componentCtx, service.config.clock, service.config.componentDrainTimeout)
		}
		err := invokeBounded(componentCtx, component.Drain)
		cancel()
		endedAt := service.config.clock.Now()
		duration := nonnegativeDuration(endedAt.Sub(beganAt))
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) || (errors.Is(err, context.Canceled) && ctx.Err() != nil) {
				timeout := service.config.componentDrainTimeout
				if ctx.Err() != nil {
					timeout = service.config.shutdownTimeout
				}
				err = shutdownTimeoutError(componentCtx, service.name, err, timeout)
			}
			wrapped := componentFailure(componentCtx, service.name, component.Name, PhaseDrain, err)
			failures = append(failures, wrapped)
			service.emit(Event{Kind: EventComponentDrained, Service: service.name, Component: component.Name, At: endedAt, Duration: duration, Err: wrapped})
			continue
		}
		service.emit(Event{Kind: EventComponentDrained, Service: service.name, Component: component.Name, At: endedAt, Duration: duration})
	}
	return errors.Join(failures...)
}

func (service *Service) stopComponents(ctx context.Context, components []Component) error {
	failures := make([]error, 0)
	for index := len(components) - 1; index >= 0; index-- {
		component := components[index]
		if err := ctx.Err(); err != nil {
			failures = append(failures, shutdownTimeoutError(ctx, service.name, err, service.config.shutdownTimeout))
			break
		}
		beganAt := service.config.clock.Now()
		service.emit(Event{Kind: EventComponentStopping, Service: service.name, Component: component.Name, At: beganAt})
		componentCtx := faults.ContextWithOperation(ctx, componentOperation(PhaseStop))
		cancel := func() {}
		if service.config.componentStopTimeout > 0 {
			componentCtx, cancel = withClockTimeout(componentCtx, service.config.clock, service.config.componentStopTimeout)
		}
		err := invokeBounded(componentCtx, component.Stop)
		cancel()
		endedAt := service.config.clock.Now()
		duration := nonnegativeDuration(endedAt.Sub(beganAt))
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) || (errors.Is(err, context.Canceled) && ctx.Err() != nil) {
				timeout := service.config.componentStopTimeout
				if ctx.Err() != nil {
					timeout = service.config.shutdownTimeout
				}
				err = shutdownTimeoutError(componentCtx, service.name, err, timeout)
			}
			wrapped := componentFailure(componentCtx, service.name, component.Name, PhaseStop, err)
			failures = append(failures, wrapped)
			service.emit(Event{Kind: EventComponentStopped, Service: service.name, Component: component.Name, At: endedAt, Duration: duration, Err: wrapped})
			continue
		}
		service.emit(Event{Kind: EventComponentStopped, Service: service.name, Component: component.Name, At: endedAt, Duration: duration})
	}
	return errors.Join(failures...)
}

func (service *Service) waitForRunLoops(ctx context.Context, done <-chan struct{}) error {
	if done == nil {
		return nil
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return shutdownTimeoutError(ctx, service.name, ctx.Err(), service.config.shutdownTimeout)
	}
}
