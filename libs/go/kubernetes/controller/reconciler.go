// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package controller

import (
	"context"
	"reflect"

	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"mindclade.internal/libs/go/faults"
)

// ReconcilerFunc adapts a function to reconcile.Reconciler.
type ReconcilerFunc func(context.Context, reconcile.Request) (reconcile.Result, error)

func (function ReconcilerFunc) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	if function == nil {
		return reconcile.Result{}, faults.New(
			faults.CodeInternal,
			"reconciler function is not configured",
			faults.WithReason("nil_reconciler_function"),
			faults.WithOperation("kubernetes.controller.Reconcile"),
			faults.WithRetryPolicy(faults.NoRetry()),
		)
	}
	return function(ctx, request)
}

// Middleware decorates a reconciler.
type Middleware func(reconcile.Reconciler) reconcile.Reconciler

// Chain applies middleware in reverse declaration order, so the first
// middleware is the outermost wrapper.
func Chain(base reconcile.Reconciler, middleware ...Middleware) (reconcile.Reconciler, error) {
	if isNil(base) {
		return nil, invalid("base reconciler is required", "nil_reconciler")
	}
	wrapped := base
	for index := len(middleware) - 1; index >= 0; index-- {
		if middleware[index] == nil {
			return nil, invalid("controller middleware is required", "nil_middleware")
		}
		wrapped = middleware[index](wrapped)
		if isNil(wrapped) {
			return nil, invalid("controller middleware returned no reconciler", "nil_wrapped_reconciler")
		}
	}
	return wrapped, nil
}

func invalid(message, reason string) error {
	return faults.New(
		faults.CodeInvalidArgument,
		message,
		faults.WithReason(reason),
		faults.WithOperation("kubernetes.controller.Chain"),
		faults.WithRetryPolicy(faults.NoRetry()),
	)
}

func isNil(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}
