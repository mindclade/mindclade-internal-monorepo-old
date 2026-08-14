// Copyright 2026 Mindclade. All rights reserved.
// Confidential and proprietary.

package controller

import (
	"context"
	"errors"
	"testing"
	"time"

	runtimecontroller "sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"mindclade.internal/libs/go/clock"
	"mindclade.internal/libs/go/faults"
	"mindclade.internal/libs/go/requestmeta"
)

func TestChainOrder(t *testing.T) {
	var calls []string
	middleware := func(name string) Middleware {
		return func(next reconcile.Reconciler) reconcile.Reconciler {
			return ReconcilerFunc(func(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
				calls = append(calls, name+":before")
				result, err := next.Reconcile(ctx, request)
				calls = append(calls, name+":after")
				return result, err
			})
		}
	}
	base := ReconcilerFunc(func(context.Context, reconcile.Request) (reconcile.Result, error) {
		calls = append(calls, "base")
		return reconcile.Result{}, nil
	})
	wrapped, err := Chain(base, middleware("one"), middleware("two"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = wrapped.Reconcile(context.Background(), reconcile.Request{})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"one:before", "two:before", "base", "two:after", "one:after"}
	if len(calls) != len(want) {
		t.Fatalf("calls = %#v", calls)
	}
	for index := range want {
		if calls[index] != want[index] {
			t.Fatalf("calls = %#v, want %#v", calls, want)
		}
	}
}

func TestRecover(t *testing.T) {
	wrapped, err := Chain(ReconcilerFunc(func(context.Context, reconcile.Request) (reconcile.Result, error) {
		panic("boom")
	}), Recover())
	if err != nil {
		t.Fatal(err)
	}
	_, err = wrapped.Reconcile(context.Background(), reconcile.Request{})
	if faults.CodeOf(err) != faults.CodeInternal || faults.ReasonOf(err) != "controller_panic" {
		t.Fatalf("error = %v", err)
	}
}

func TestRequestMetadataAndObserver(t *testing.T) {
	operation := requestmeta.MustParseOperation("controllers.runs.Reconcile")
	metadataMiddleware, err := RequestMetadata(operation)
	if err != nil {
		t.Fatal(err)
	}
	fakeClock := clock.NewFake(time.Unix(100, 0))
	var observed Event
	observerMiddleware, err := Observe(fakeClock, ObserverFunc(func(_ context.Context, event Event) { observed = event }))
	if err != nil {
		t.Fatal(err)
	}
	base := ReconcilerFunc(func(ctx context.Context, _ reconcile.Request) (reconcile.Result, error) {
		if _, ok := requestmeta.RequestIDFromContext(ctx); !ok {
			return reconcile.Result{}, errors.New("request ID missing")
		}
		if got, ok := requestmeta.OperationFromContext(ctx); !ok || got != operation {
			return reconcile.Result{}, errors.New("operation missing")
		}
		return reconcile.Result{Requeue: true}, nil
	})
	wrapped, err := Chain(base, observerMiddleware, metadataMiddleware)
	if err != nil {
		t.Fatal(err)
	}
	result, err := wrapped.Reconcile(context.Background(), reconcile.Request{})
	if err != nil || !result.Requeue || !observed.Result.Requeue {
		t.Fatalf("result=%#v observed=%#v err=%v", result, observed, err)
	}
}

func TestSettingsApply(t *testing.T) {
	recoverPanic := true
	settings := Settings{MaxConcurrentReconciles: 4, ReconciliationTimeout: time.Minute, RecoverPanic: &recoverPanic}
	var options runtimecontroller.Options
	if err := settings.Apply(&options); err != nil {
		t.Fatal(err)
	}
	if options.MaxConcurrentReconciles != 4 || options.ReconciliationTimeout != time.Minute || options.RecoverPanic == nil || !*options.RecoverPanic {
		t.Fatalf("options = %#v", options)
	}
	recoverPanic = false
	if options.RecoverPanic == nil || !*options.RecoverPanic {
		t.Fatal("applied options alias caller-owned pointer")
	}
}
