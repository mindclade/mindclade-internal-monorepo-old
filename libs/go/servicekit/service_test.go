// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package servicekit

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"mindclade.internal/libs/go/faults"
)

func TestServiceLifecycleOrderAndGracefulShutdown(t *testing.T) {
	t.Parallel()

	var orderMu sync.Mutex
	var order []string
	record := func(value string) {
		orderMu.Lock()
		order = append(order, value)
		orderMu.Unlock()
	}

	running := make(chan struct{})
	var runningOnce sync.Once
	service, err := New(
		"control-plane",
		WithStartupTimeout(time.Second),
		WithShutdownTimeout(time.Second),
		WithComponentStopTimeout(time.Second),
		WithObserver(ObserverFunc(func(event Event) {
			if event.Kind == EventStateChanged && event.To == StateRunning {
				runningOnce.Do(func() { close(running) })
			}
		})),
	)
	if err != nil {
		t.Fatalf("New returned %v", err)
	}

	for _, name := range []string{"database", "api"} {
		name := name
		if err := service.Add(Component{
			Name: name,
			Start: func(context.Context) error {
				record("start:" + name)
				return nil
			},
			Run: func(ctx context.Context) error {
				<-ctx.Done()
				return ctx.Err()
			},
			Stop: func(context.Context) error {
				record("stop:" + name)
				return nil
			},
		}); err != nil {
			t.Fatalf("Add(%s) returned %v", name, err)
		}
	}

	runResult := make(chan error, 1)
	go func() { runResult <- service.Run(context.Background()) }()

	select {
	case <-running:
	case <-time.After(time.Second):
		t.Fatal("service did not reach running state")
	}
	if report := service.Liveness(context.Background()); !report.OK {
		t.Fatalf("liveness while running = %+v, err=%v", report, report.Err())
	}
	if report := service.Readiness(context.Background()); !report.OK {
		t.Fatalf("readiness while running = %+v, err=%v", report, report.Err())
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := service.Shutdown(shutdownCtx); err != nil {
		t.Fatalf("Shutdown returned %v", err)
	}
	if err := <-runResult; err != nil {
		t.Fatalf("Run returned %v", err)
	}

	orderMu.Lock()
	gotOrder := append([]string(nil), order...)
	orderMu.Unlock()
	wantOrder := []string{"start:database", "start:api", "stop:api", "stop:database"}
	if !reflect.DeepEqual(gotOrder, wantOrder) {
		t.Fatalf("lifecycle order = %v, want %v", gotOrder, wantOrder)
	}
	if snapshot := service.Snapshot(); snapshot.State != StateStopped || snapshot.Cause != nil {
		t.Fatalf("final snapshot = %+v", snapshot)
	}
	if report := service.Readiness(context.Background()); report.OK || faults.ReasonOf(report.Err()) != "service_not_ready" {
		t.Fatalf("readiness after stop = %+v, err=%v", report, report.Err())
	}

	if err := service.Add(Component{Name: "late", Run: func(context.Context) error { return nil }}); !errors.Is(err, ErrConfigurationFrozen) {
		t.Fatalf("late Add error = %v, want ErrConfigurationFrozen", err)
	} else if faults.CodeOf(err) != faults.CodeFailedPrecondition {
		t.Fatalf("late Add code = %s", faults.CodeOf(err))
	}
	if err := service.Run(context.Background()); !errors.Is(err, ErrAlreadyRun) {
		t.Fatalf("second Run error = %v, want ErrAlreadyRun", err)
	} else if faults.CodeOf(err) != faults.CodeFailedPrecondition {
		t.Fatalf("second Run code = %s", faults.CodeOf(err))
	}
}

func TestServiceStartupFailureRollsBackStartedComponents(t *testing.T) {
	t.Parallel()

	expectedFailure := errors.New("database unavailable")
	var mu sync.Mutex
	var order []string
	record := func(value string) {
		mu.Lock()
		order = append(order, value)
		mu.Unlock()
	}

	service, err := New("api", WithShutdownTimeout(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Add(Component{
		Name: "configuration",
		Start: func(context.Context) error {
			record("start:configuration")
			return nil
		},
		Stop: func(context.Context) error {
			record("stop:configuration")
			return nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := service.Add(Component{
		Name: "database",
		Start: func(context.Context) error {
			record("start:database")
			return expectedFailure
		},
		Stop: func(context.Context) error {
			record("stop:database")
			return nil
		},
	}); err != nil {
		t.Fatal(err)
	}

	runErr := service.Run(context.Background())
	if !errors.Is(runErr, expectedFailure) {
		t.Fatalf("Run error = %v, want underlying startup failure", runErr)
	}
	if faults.CodeOf(runErr) != faults.CodeUnavailable || faults.ReasonOf(runErr) != "component_start_failed" {
		t.Fatalf("classification = %s/%q", faults.CodeOf(runErr), faults.ReasonOf(runErr))
	}
	fields := faults.FieldsOf(runErr)
	if fields[FieldServiceName] != "api" || fields[FieldComponentName] != "database" || fields[FieldLifecyclePhase] != "start" {
		t.Fatalf("unexpected fields: %#v", fields)
	}
	var componentErr *ComponentError
	if !errors.As(runErr, &componentErr) || componentErr.Component != "database" || componentErr.Phase != PhaseStart {
		t.Fatalf("unexpected ComponentError: %#v", componentErr)
	}
	mu.Lock()
	gotOrder := append([]string(nil), order...)
	mu.Unlock()
	wantOrder := []string{"start:configuration", "start:database", "stop:configuration"}
	if !reflect.DeepEqual(gotOrder, wantOrder) {
		t.Fatalf("rollback order = %v, want %v", gotOrder, wantOrder)
	}
	if snapshot := service.Snapshot(); snapshot.State != StateFailed || snapshot.Cause == nil {
		t.Fatalf("final snapshot = %+v", snapshot)
	}
}

func TestServiceRunFailurePreservesFaultMetadata(t *testing.T) {
	t.Parallel()

	running := make(chan struct{})
	var runningOnce sync.Once
	service, err := New(
		"inference-worker",
		WithShutdownTimeout(time.Second),
		WithObserver(ObserverFunc(func(event Event) {
			if event.Kind == EventStateChanged && event.To == StateRunning {
				runningOnce.Do(func() { close(running) })
			}
		})),
	)
	if err != nil {
		t.Fatal(err)
	}

	if err := service.Add(Component{
		Name: "server",
		Run: func(ctx context.Context) error {
			<-ctx.Done()
			return ctx.Err()
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := service.Add(Component{
		Name: "model-runtime",
		Run: func(context.Context) error {
			return faults.New(
				faults.CodeDataLoss,
				"model state is corrupt",
				faults.WithReason("model_state_corrupt"),
				faults.WithField("model_id", "clade-1"),
			)
		},
	}); err != nil {
		t.Fatal(err)
	}

	parent := faults.ContextWithRequestID(context.Background(), "req_123")
	parent = faults.ContextWithTraceID(parent, "trace_456")
	runErr := service.Run(parent)
	select {
	case <-running:
	default:
		t.Fatal("service never emitted running state")
	}
	if faults.CodeOf(runErr) != faults.CodeDataLoss || faults.ReasonOf(runErr) != "model_state_corrupt" {
		t.Fatalf("classification = %s/%q; error=%v", faults.CodeOf(runErr), faults.ReasonOf(runErr), runErr)
	}
	fields := faults.FieldsOf(runErr)
	if fields[FieldComponentName] != "model-runtime" || fields[FieldLifecyclePhase] != "run" ||
		fields["model_id"] != "clade-1" || fields[faults.FieldRequestID] != "req_123" ||
		fields[faults.FieldTraceID] != "trace_456" {
		t.Fatalf("unexpected fields: %#v", fields)
	}
	if snapshot := service.Snapshot(); snapshot.State != StateFailed {
		t.Fatalf("final state = %s", snapshot.State)
	}
}

func TestServiceStartupTimeout(t *testing.T) {
	t.Parallel()

	service, err := New(
		"slow-start",
		WithStartupTimeout(20*time.Millisecond),
		WithShutdownTimeout(time.Second),
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Add(Component{
		Name: "dependency",
		Start: func(ctx context.Context) error {
			<-ctx.Done()
			return ctx.Err()
		},
	}); err != nil {
		t.Fatal(err)
	}

	runErr := service.Run(context.Background())
	if !errors.Is(runErr, ErrStartupTimeout) || !errors.Is(runErr, context.DeadlineExceeded) {
		t.Fatalf("Run error = %v, want startup/deadline timeout", runErr)
	}
	if faults.CodeOf(runErr) != faults.CodeDeadlineExceeded || faults.ReasonOf(runErr) != "startup_timeout" {
		t.Fatalf("classification = %s/%q", faults.CodeOf(runErr), faults.ReasonOf(runErr))
	}
}

func TestServiceShutdownTimeoutWhenRunLoopIgnoresCancellation(t *testing.T) {
	t.Parallel()

	entered := make(chan struct{})
	release := make(chan struct{})
	service, err := New(
		"stuck-worker",
		WithShutdownTimeout(25*time.Millisecond),
		WithComponentStopTimeout(0),
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Add(Component{
		Name: "worker",
		Run: func(context.Context) error {
			close(entered)
			<-release
			return nil
		},
	}); err != nil {
		t.Fatal(err)
	}

	runResult := make(chan error, 1)
	go func() { runResult <- service.Run(context.Background()) }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("run loop did not start")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	shutdownErr := service.Shutdown(shutdownCtx)
	if !errors.Is(shutdownErr, ErrShutdownTimeout) || !errors.Is(shutdownErr, context.DeadlineExceeded) {
		t.Fatalf("Shutdown error = %v, want shutdown/deadline timeout", shutdownErr)
	}
	if faults.CodeOf(shutdownErr) != faults.CodeDeadlineExceeded || faults.ReasonOf(shutdownErr) != "shutdown_timeout" {
		t.Fatalf("classification = %s/%q", faults.CodeOf(shutdownErr), faults.ReasonOf(shutdownErr))
	}
	if runErr := <-runResult; !errors.Is(runErr, ErrShutdownTimeout) {
		t.Fatalf("Run error = %v, want ErrShutdownTimeout", runErr)
	}
	close(release)
	if snapshot := service.Snapshot(); snapshot.State != StateFailed {
		t.Fatalf("final state = %s", snapshot.State)
	}
}

func TestServiceWaitContextFailureIsStructured(t *testing.T) {
	t.Parallel()

	service, err := New("not-running")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Millisecond)
	defer cancel()
	waitErr := service.Wait(ctx)
	if !errors.Is(waitErr, context.DeadlineExceeded) {
		t.Fatalf("Wait error = %v", waitErr)
	}
	if faults.CodeOf(waitErr) != faults.CodeDeadlineExceeded ||
		faults.ReasonOf(waitErr) != "service_context_deadline_exceeded" ||
		faults.OperationOf(waitErr) != operationWait {
		t.Fatalf("classification = %s/%q/%q", faults.CodeOf(waitErr), faults.ReasonOf(waitErr), faults.OperationOf(waitErr))
	}
}

func TestNilServiceOperationsAreStructured(t *testing.T) {
	t.Parallel()

	var service *Service
	for name, err := range map[string]error{
		"add":      service.Add(Component{Name: "worker", Run: func(context.Context) error { return nil }}),
		"wait":     service.Wait(context.Background()),
		"shutdown": service.Shutdown(context.Background()),
		"run":      service.Run(context.Background()),
	} {
		if !errors.Is(err, ErrNilService) || faults.CodeOf(err) != faults.CodeInvalidArgument {
			t.Fatalf("%s error = %v (%s)", name, err, faults.CodeOf(err))
		}
	}
}

func TestServicePublicRegistriesAndDone(t *testing.T) {
	t.Parallel()

	running := make(chan struct{})
	var runningOnce sync.Once
	service, err := New(
		"registry-service",
		WithObserver(ObserverFunc(func(event Event) {
			if event.Kind == EventStateChanged && event.To == StateRunning {
				runningOnce.Do(func() { close(running) })
			}
		})),
	)
	if err != nil {
		t.Fatal(err)
	}
	if service.Name() != "registry-service" || len(service.Components()) != 0 {
		t.Fatalf("unexpected initial API state: name=%q components=%v", service.Name(), service.Components())
	}
	select {
	case <-service.Done():
		t.Fatal("Done closed before Run completed")
	default:
	}

	if err := service.Add(Component{
		Name:      "passive",
		Liveness:  func(context.Context) error { return nil },
		Readiness: func(context.Context) error { return nil },
	}); err != nil {
		t.Fatal(err)
	}
	if got := service.Components(); !reflect.DeepEqual(got, []string{"passive"}) {
		t.Fatalf("Components() = %v", got)
	}
	if err := service.RegisterLiveness("dependency/cache", func(context.Context) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if err := service.RegisterReadiness("dependency/cache", func(context.Context) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if !service.UnregisterLiveness("dependency/cache") || service.UnregisterLiveness("dependency/cache") {
		t.Fatal("UnregisterLiveness existence reporting failed")
	}
	if !service.UnregisterReadiness("dependency/cache") || service.UnregisterReadiness("dependency/cache") {
		t.Fatal("UnregisterReadiness existence reporting failed")
	}

	parent, cancelParent := context.WithCancel(context.Background())
	runResult := make(chan error, 1)
	go func() { runResult <- service.Run(parent) }()
	select {
	case <-running:
	case <-time.After(time.Second):
		t.Fatal("service did not become running")
	}
	cancelParent()
	if err := <-runResult; err != nil {
		t.Fatalf("Run returned %v", err)
	}
	select {
	case <-service.Done():
	case <-time.After(time.Second):
		t.Fatal("Done did not close")
	}
}

func TestServiceStopFailuresAreAggregated(t *testing.T) {
	t.Parallel()

	firstFailure := errors.New("first stop failed")
	secondFailure := errors.New("second stop failed")
	service, err := New("stop-errors", WithShutdownTimeout(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Add(Component{
		Name: "first",
		Run:  func(context.Context) error { return nil },
		Stop: func(context.Context) error { return firstFailure },
	}); err != nil {
		t.Fatal(err)
	}
	if err := service.Add(Component{
		Name: "second",
		Stop: func(context.Context) error { return secondFailure },
	}); err != nil {
		t.Fatal(err)
	}

	runErr := service.Run(context.Background())
	if !errors.Is(runErr, firstFailure) || !errors.Is(runErr, secondFailure) {
		t.Fatalf("Run error did not aggregate stop failures: %v", runErr)
	}
	if faults.CodeOf(runErr) != faults.CodeInternal || faults.ReasonOf(runErr) != "component_stop_failed" {
		t.Fatalf("classification = %s/%q", faults.CodeOf(runErr), faults.ReasonOf(runErr))
	}
}

func TestServiceComponentStopTimeoutContinuesShutdown(t *testing.T) {
	t.Parallel()

	entered := make(chan struct{})
	release := make(chan struct{})
	var stoppedSecond bool
	service, err := New(
		"stop-timeout",
		WithShutdownTimeout(time.Second),
		WithComponentStopTimeout(20*time.Millisecond),
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Add(Component{
		Name: "second",
		Stop: func(context.Context) error {
			stoppedSecond = true
			return nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := service.Add(Component{
		Name: "stuck",
		Run: func(context.Context) error {
			return nil
		},
		Stop: func(context.Context) error {
			close(entered)
			<-release
			return nil
		},
	}); err != nil {
		t.Fatal(err)
	}

	runErr := service.Run(context.Background())
	select {
	case <-entered:
	default:
		t.Fatal("stuck Stop was not entered")
	}
	if !errors.Is(runErr, ErrShutdownTimeout) || faults.CodeOf(runErr) != faults.CodeDeadlineExceeded {
		t.Fatalf("Run error = %v (%s)", runErr, faults.CodeOf(runErr))
	}
	if !stoppedSecond {
		t.Fatal("shutdown did not continue after per-component timeout")
	}
	close(release)
}
