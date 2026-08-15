// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package controller

import (
	"context"
	"fmt"
	"runtime/debug"

	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"mindclade.internal/libs/go/faults"
)

// Recover contains panics from a reconciler and converts them to internal
// faults while retaining the stack trace only in the wrapped diagnostic cause.
func Recover() Middleware {
	return func(next reconcile.Reconciler) reconcile.Reconciler {
		return ReconcilerFunc(func(ctx context.Context, request reconcile.Request) (result reconcile.Result, err error) {
			defer func() {
				if recovered := recover(); recovered != nil {
					cause := fmt.Errorf("controller panic: %v\n%s", recovered, debug.Stack())
					err = faults.Wrap(
						cause,
						faults.CodeInternal,
						"controller reconciliation failed",
						faults.WithReason("controller_panic"),
						faults.WithOperation("kubernetes.controller.Reconcile"),
						faults.WithFields(requestFields(request)),
						faults.WithContextMetadata(ctx),
						faults.WithRetryPolicy(faults.NoRetry()),
					)
					result = reconcile.Result{}
				}
			}()
			return next.Reconcile(ctx, request)
		})
	}
}
