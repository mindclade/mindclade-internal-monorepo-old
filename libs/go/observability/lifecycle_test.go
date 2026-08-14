// Copyright 2026 Mindclade. All rights reserved.
// Confidential and proprietary.

package observability

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"mindclade.internal/libs/go/faults"
)

func TestPipelineFlushAndShutdownOrder(t *testing.T) {
	var mu sync.Mutex
	var calls []string
	record := func(value string) LifecycleHook {
		return func(context.Context) error {
			mu.Lock()
			calls = append(calls, value)
			mu.Unlock()
			return nil
		}
	}
	pipeline, err := NewPipeline(
		LifecycleComponent{Name: "traces", ForceFlush: record("flush:traces"), Shutdown: record("shutdown:traces")},
		LifecycleComponent{Name: "metrics", ForceFlush: record("flush:metrics"), Shutdown: record("shutdown:metrics")},
		LifecycleComponent{Name: "logs", ForceFlush: record("flush:logs"), Shutdown: record("shutdown:logs")},
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := pipeline.ForceFlush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := pipeline.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"flush:traces", "flush:metrics", "flush:logs",
		"shutdown:logs", "shutdown:metrics", "shutdown:traces",
	}
	mu.Lock()
	got := append([]string(nil), calls...)
	mu.Unlock()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("calls = %#v, want %#v", got, want)
	}
	if err := pipeline.Shutdown(context.Background()); err != nil {
		t.Fatalf("second Shutdown() = %v", err)
	}
	if err := pipeline.Add(LifecycleComponent{Name: "late", Shutdown: func(context.Context) error { return nil }}); !errors.Is(err, ErrPipelineClosed) {
		t.Fatalf("Add after shutdown error = %v", err)
	}
	if err := pipeline.ForceFlush(context.Background()); !errors.Is(err, ErrPipelineClosed) {
		t.Fatalf("ForceFlush after shutdown error = %v", err)
	}
}

func TestPipelineValidationAndFailureAggregation(t *testing.T) {
	pipeline := &Pipeline{}
	if err := pipeline.Add(LifecycleComponent{Name: ""}); !errors.Is(err, ErrInvalidComponent) {
		t.Fatalf("Add(empty) error = %v", err)
	}
	component := LifecycleComponent{Name: "metrics", Shutdown: func(context.Context) error { return errors.New("shutdown failed") }}
	if err := pipeline.Add(component); err != nil {
		t.Fatal(err)
	}
	if err := pipeline.Add(component); !errors.Is(err, ErrDuplicateComponent) {
		t.Fatalf("Add(duplicate) error = %v", err)
	}
	if err := pipeline.Add(LifecycleComponent{Name: "traces", Shutdown: func(context.Context) error { panic("boom") }}); err != nil {
		t.Fatal(err)
	}
	err := pipeline.Shutdown(context.Background())
	if err == nil || faults.CodeOf(err) == faults.CodeUnknown {
		t.Fatalf("Shutdown() error = %v, code = %s", err, faults.CodeOf(err))
	}
	if !errors.Is(err, ErrProviderPanic) || !stringsContain(err.Error(), "shutdown failed") {
		t.Fatalf("aggregated error = %v", err)
	}
}

func TestPipelineConcurrentShutdownAndFlushRejection(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	pipeline, err := NewPipeline(LifecycleComponent{
		Name: "blocking",
		Shutdown: func(context.Context) error {
			close(entered)
			<-release
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	first := make(chan error, 1)
	go func() { first <- pipeline.Shutdown(context.Background()) }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("shutdown hook did not start")
	}
	if err := pipeline.ForceFlush(context.Background()); !errors.Is(err, ErrPipelineClosed) {
		t.Fatalf("ForceFlush while shutting error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := pipeline.Shutdown(ctx); faults.CodeOf(err) != faults.CodeCanceled {
		t.Fatalf("concurrent Shutdown(canceled) error = %v, code=%s", err, faults.CodeOf(err))
	}
	close(release)
	if err := <-first; err != nil {
		t.Fatalf("first Shutdown() error = %v", err)
	}
}

func TestPipelineNilAndContextContracts(t *testing.T) {
	var pipeline *Pipeline
	if err := pipeline.Add(LifecycleComponent{Name: "x", Shutdown: func(context.Context) error { return nil }}); !errors.Is(err, ErrInvalidComponent) {
		t.Fatalf("nil Add error = %v", err)
	}
	if err := pipeline.ForceFlush(context.Background()); !errors.Is(err, ErrInvalidComponent) {
		t.Fatalf("nil ForceFlush error = %v", err)
	}
	if err := (&Pipeline{}).ForceFlush(nil); !errors.Is(err, ErrNilContext) {
		t.Fatalf("ForceFlush(nil) error = %v", err)
	}
}

func stringsContain(value, substring string) bool {
	for index := 0; index+len(substring) <= len(value); index++ {
		if value[index:index+len(substring)] == substring {
			return true
		}
	}
	return substring == ""
}

func TestPipelineLifecycleHookIsBoundedByContext(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	pipeline, err := NewPipeline(LifecycleComponent{
		Name: "ignores-cancellation",
		ForceFlush: func(context.Context) error {
			close(entered)
			<-release
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- pipeline.ForceFlush(ctx) }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("flush hook did not start")
	}
	cancel()
	select {
	case flushErr := <-done:
		if faults.CodeOf(flushErr) != faults.CodeCanceled {
			t.Fatalf("ForceFlush error = %v, code = %s", flushErr, faults.CodeOf(flushErr))
		}
	case <-time.After(time.Second):
		t.Fatal("ForceFlush remained blocked after context cancellation")
	}
	close(release)
}
