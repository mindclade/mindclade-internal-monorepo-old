// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package servicekit

import (
	"context"
	"errors"
	"sync"
	"time"

	"go.mindclade.dev/libs/go/faults"
)

const lifecycleProbeName = "service/lifecycle"

// Service coordinates the lifecycle of a set of components.
//
// A Service is safe for concurrent observation, probe execution, Shutdown, and
// Wait. Component registration is allowed only before the first Run call. A
// Service is intentionally single-use so lifecycle state and shutdown ordering
// remain unambiguous.
type Service struct {
	name   string
	config configuration

	mu             sync.RWMutex
	state          State
	stateSince     time.Time
	cause          error
	components     []Component
	componentNames map[string]struct{}
	runStarted     bool
	cancel         context.CancelCauseFunc
	shutdown       chan error
	done           chan struct{}
	result         error

	liveness  *ProbeSet
	readiness *ProbeSet
}

// New constructs a service with validated lifecycle settings.
func New(name string, options ...Option) (*Service, error) {
	if err := validateName("service", name, operationNew); err != nil {
		return nil, err
	}
	config := defaultConfiguration()
	for _, option := range options {
		if option == nil {
			continue
		}
		if err := option(&config); err != nil {
			return nil, err
		}
	}
	if nilInterface(config.clock) {
		return nil, structuredFault(nil, ErrNilClock, faults.CodeInvalidArgument, "service clock must not be nil", "nil_clock", operationNew, nil)
	}
	now := config.clock.Now()
	return &Service{
		name: name, config: config, state: StateNew, stateSince: now,
		componentNames: make(map[string]struct{}), shutdown: make(chan error, 1), done: make(chan struct{}),
		liveness: newProbeSet(config.probeTimeout, config.clock), readiness: newProbeSet(config.probeTimeout, config.clock),
	}, nil
}

func (service *Service) Name() string {
	if service == nil {
		return ""
	}
	return service.name
}

// Add registers a component. Components start in registration order and drain
// and stop in reverse registration order.
func (service *Service) Add(component Component) error {
	if service == nil {
		return nilServiceError(operationAdd)
	}
	if err := component.validate(); err != nil {
		return err
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	if service.runStarted || service.state != StateNew {
		return configurationFrozenError(service.name)
	}
	if _, exists := service.componentNames[component.Name]; exists {
		return duplicateComponentError(service.name, component.Name)
	}
	probeName := componentProbeName(component.Name)
	livenessRegistered := false
	if component.Liveness != nil {
		if err := service.liveness.Register(probeName, component.Liveness); err != nil {
			return err
		}
		livenessRegistered = true
	}
	if component.Readiness != nil {
		if err := service.readiness.Register(probeName, component.Readiness); err != nil {
			if livenessRegistered {
				service.liveness.Unregister(probeName)
			}
			return err
		}
	}
	service.components = append(service.components, component)
	service.componentNames[component.Name] = struct{}{}
	return nil
}

func (service *Service) Components() []string {
	if service == nil {
		return nil
	}
	service.mu.RLock()
	defer service.mu.RUnlock()
	names := make([]string, len(service.components))
	for index, component := range service.components {
		names[index] = component.Name
	}
	return names
}

func (service *Service) RegisterLiveness(name string, probe Probe) error {
	if service == nil {
		return nilServiceError(operationProbeRegister)
	}
	return service.liveness.Register(name, probe)
}
func (service *Service) RegisterReadiness(name string, probe Probe) error {
	if service == nil {
		return nilServiceError(operationProbeRegister)
	}
	return service.readiness.Register(name, probe)
}
func (service *Service) UnregisterLiveness(name string) bool {
	return service != nil && service.liveness.Unregister(name)
}
func (service *Service) UnregisterReadiness(name string) bool {
	return service != nil && service.readiness.Unregister(name)
}

func (service *Service) Snapshot() Snapshot {
	if service == nil {
		return Snapshot{State: StateFailed, Cause: nilServiceError("servicekit.Service.Snapshot")}
	}
	service.mu.RLock()
	defer service.mu.RUnlock()
	return Snapshot{Name: service.name, State: service.state, Since: service.stateSince, Cause: service.cause}
}
func (service *Service) Done() <-chan struct{} {
	if service == nil {
		closed := make(chan struct{})
		close(closed)
		return closed
	}
	return service.done
}
func (service *Service) Wait(ctx context.Context) error {
	if ctx == nil {
		return nilContextError(operationWait)
	}
	if service == nil {
		return nilServiceError(operationWait)
	}
	select {
	case <-service.done:
		service.mu.RLock()
		result := service.result
		service.mu.RUnlock()
		return result
	case <-ctx.Done():
		return contextError(ctx, ctx.Err(), operationWait, service.name)
	}
}

// Shutdown requests graceful drain and waits for Run to finish. It does not
// cancel component Run contexts until every Drain hook has been attempted.
func (service *Service) Shutdown(ctx context.Context) error {
	if ctx == nil {
		return nilContextError(operationShutdown)
	}
	if service == nil {
		return nilServiceError(operationShutdown)
	}
	service.mu.RLock()
	started := service.runStarted
	service.mu.RUnlock()
	if !started {
		return nil
	}
	select {
	case service.shutdown <- errShutdownRequested:
	default:
	}
	return service.Wait(ctx)
}

func (service *Service) Liveness(ctx context.Context) ProbeReport {
	if service == nil {
		now := time.Now()
		return ProbeReport{OK: false, CheckedAt: now, Results: []ProbeResult{{Name: lifecycleProbeName, OK: false, CheckedAt: now, Err: stateFailure("liveness", StateFailed)}}}
	}
	snapshot := service.Snapshot()
	checkedAt := service.config.clock.Now()
	live := snapshot.State == StateStarting || snapshot.State == StateRunning || snapshot.State == StateDraining || snapshot.State == StateStopping
	base := ProbeResult{Name: lifecycleProbeName, OK: live, CheckedAt: checkedAt}
	if !live {
		base.Err = stateFailure("liveness", snapshot.State)
	}
	return combineProbeReports(base, service.liveness.Check(ctx), service.config.clock.Now)
}
func (service *Service) Readiness(ctx context.Context) ProbeReport {
	if service == nil {
		now := time.Now()
		return ProbeReport{OK: false, CheckedAt: now, Results: []ProbeResult{{Name: lifecycleProbeName, OK: false, CheckedAt: now, Err: stateFailure("readiness", StateFailed)}}}
	}
	snapshot := service.Snapshot()
	checkedAt := service.config.clock.Now()
	ready := snapshot.State == StateRunning
	base := ProbeResult{Name: lifecycleProbeName, OK: ready, CheckedAt: checkedAt}
	if !ready {
		base.Err = stateFailure("readiness", snapshot.State)
	}
	return combineProbeReports(base, service.readiness.Check(ctx), service.config.clock.Now)
}

// Run starts, supervises, drains, and stops the service. Parent cancellation is
// a graceful termination request: values are preserved, but component Run
// contexts are detached so Drain hooks execute before cancellation.
func (service *Service) Run(parent context.Context) (result error) {
	if parent == nil {
		return nilContextError(operationRun)
	}
	if service == nil {
		return nilServiceError(operationRun)
	}

	startupCtx := faults.ContextWithOperation(parent, operationRun)
	runCtx, cancel, components, err := service.beginRun(parent)
	if err != nil {
		return err
	}
	defer func() { cancel(errShutdownRequested); service.finish(result) }()

	service.transition(StateStarting, nil)
	started, startErr := service.startComponents(startupCtx, components)
	if startErr != nil {
		if startupCtx.Err() != nil && errors.Is(startErr, startupCtx.Err()) && !errors.Is(startErr, ErrStartupTimeout) {
			startErr = nil
		}
		cancel(startErr)
		service.transition(StateStopping, startErr)
		shutdownCtx, shutdownCancel := service.newShutdownContext(parent)
		stopErr := service.stopComponents(shutdownCtx, started)
		shutdownCancel()
		result = errors.Join(startErr, stopErr)
		service.finishState(result)
		return result
	}

	if parent.Err() != nil {
		shutdownCtx, shutdownCancel := service.newShutdownContext(parent)
		service.transition(StateDraining, nil)
		drainErr := service.drainComponents(shutdownCtx, started)
		cancel(context.Cause(parent))
		service.transition(StateStopping, drainErr)
		stopErr := service.stopComponents(shutdownCtx, started)
		shutdownCancel()
		result = errors.Join(drainErr, stopErr)
		service.finishState(result)
		return result
	}

	service.transition(StateRunning, nil)
	results, runsDone, runCount := service.launchRuns(runCtx, started)

	var runErr error
	graceful := true
	if runCount == 0 {
		select {
		case <-parent.Done():
		case <-service.shutdown:
		}
	} else {
		select {
		case <-parent.Done():
		case <-service.shutdown:
		case returned := <-results:
			gracefulCancellation := runCtx.Err() != nil && (errors.Is(returned.err, context.Canceled) || errors.Is(returned.err, context.DeadlineExceeded))
			if returned.err != nil && !gracefulCancellation {
				runErr = returned.err
				graceful = false
				cancel(runErr)
			}
		}
	}

	shutdownCtx, shutdownCancel := service.newShutdownContext(parent)
	var drainErr error
	if graceful {
		service.transition(StateDraining, runErr)
		drainErr = service.drainComponents(shutdownCtx, started)
		cancel(errShutdownRequested)
	}
	service.transition(StateStopping, errors.Join(runErr, drainErr))
	stopErr := service.stopComponents(shutdownCtx, started)
	waitErr := service.waitForRunLoops(shutdownCtx, runsDone)
	shutdownCancel()

	result = errors.Join(runErr, drainErr, stopErr, waitErr)
	service.finishState(result)
	return result
}

func (service *Service) beginRun(parent context.Context) (context.Context, context.CancelCauseFunc, []Component, error) {
	service.mu.Lock()
	defer service.mu.Unlock()
	if service.runStarted {
		return nil, nil, nil, alreadyRunError(service.name)
	}
	detached := context.WithoutCancel(parent)
	operationCtx := faults.ContextWithOperation(detached, operationRun)
	runCtx, cancel := context.WithCancelCause(operationCtx)
	service.runStarted = true
	service.cancel = cancel
	return runCtx, cancel, append([]Component(nil), service.components...), nil
}

func (service *Service) startComponents(ctx context.Context, components []Component) ([]Component, error) {
	startupCtx := ctx
	cancel := func() {}
	if service.config.startupTimeout > 0 {
		startupCtx, cancel = withClockTimeout(ctx, service.config.clock, service.config.startupTimeout)
	}
	defer cancel()
	started := make([]Component, 0, len(components))
	for _, component := range components {
		if err := startupCtx.Err(); err != nil {
			return started, service.startupContextError(ctx, startupCtx, err)
		}
		beganAt := service.config.clock.Now()
		service.emit(Event{Kind: EventComponentStarting, Service: service.name, Component: component.Name, At: beganAt})
		hookCtx := faults.ContextWithOperation(startupCtx, componentOperation(PhaseStart))
		err := invokeBounded(hookCtx, component.Start)
		endedAt := service.config.clock.Now()
		duration := nonnegativeDuration(endedAt.Sub(beganAt))
		if err != nil {
			err = service.startupContextError(ctx, startupCtx, err)
			wrapped := componentFailure(hookCtx, service.name, component.Name, PhaseStart, err)
			service.emit(Event{Kind: EventComponentStarted, Service: service.name, Component: component.Name, At: endedAt, Duration: duration, Err: wrapped})
			return started, wrapped
		}
		started = append(started, component)
		service.emit(Event{Kind: EventComponentStarted, Service: service.name, Component: component.Name, At: endedAt, Duration: duration})
	}
	return started, nil
}

func (service *Service) startupContextError(runCtx, startupCtx context.Context, err error) error {
	if errors.Is(err, context.DeadlineExceeded) && startupCtx.Err() != nil && runCtx.Err() == nil {
		return startupTimeoutError(runCtx, service.name, err, service.config.startupTimeout)
	}
	if (errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)) && runCtx.Err() != nil {
		return contextError(runCtx, err, operationRun, service.name)
	}
	return err
}

type componentResult struct {
	component string
	err       error
}

func (service *Service) launchRuns(ctx context.Context, components []Component) (<-chan componentResult, <-chan struct{}, int) {
	count := 0
	for _, component := range components {
		if component.Run != nil {
			count++
		}
	}
	results := make(chan componentResult, count)
	done := make(chan struct{})
	if count == 0 {
		close(done)
		return results, done, 0
	}
	var group sync.WaitGroup
	group.Add(count)
	for _, component := range components {
		if component.Run == nil {
			continue
		}
		component := component
		go func() {
			defer group.Done()
			beganAt := service.config.clock.Now()
			service.emit(Event{Kind: EventComponentRunning, Service: service.name, Component: component.Name, At: beganAt})
			hookCtx := faults.ContextWithOperation(ctx, componentOperation(PhaseRun))
			err := invoke(hookCtx, component.Run)
			if err != nil {
				err = componentFailure(hookCtx, service.name, component.Name, PhaseRun, err)
			}
			endedAt := service.config.clock.Now()
			service.emit(Event{Kind: EventComponentExited, Service: service.name, Component: component.Name, At: endedAt, Duration: nonnegativeDuration(endedAt.Sub(beganAt)), Err: err})
			results <- componentResult{component: component.Name, err: err}
		}()
	}
	go func() { group.Wait(); close(done); close(results) }()
	return results, done, count
}

func (service *Service) finishState(result error) {
	if result != nil {
		service.transition(StateFailed, result)
	} else {
		service.transition(StateStopped, nil)
	}
}
func (service *Service) finish(result error) {
	service.mu.Lock()
	service.result = result
	service.cancel = nil
	service.mu.Unlock()
	close(service.done)
}
func (service *Service) transition(to State, cause error) {
	at := service.config.clock.Now()
	service.mu.Lock()
	from := service.state
	service.state = to
	service.stateSince = at
	switch to {
	case StateFailed:
		service.cause = cause
	case StateStopped:
		service.cause = nil
	default:
		if cause != nil {
			service.cause = cause
		}
	}
	service.mu.Unlock()
	service.emit(Event{Kind: EventStateChanged, Service: service.name, From: from, To: to, At: at, Err: cause})
}
func (service *Service) emit(event Event)        { _ = safeObserve(service.config.observer, event) }
func componentProbeName(component string) string { return "component/" + component }
func nonnegativeDuration(duration time.Duration) time.Duration {
	if duration < 0 {
		return 0
	}
	return duration
}
