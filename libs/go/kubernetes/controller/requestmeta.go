// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package controller

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"mindclade.internal/libs/go/requestmeta"
)

// RequestMetadata establishes request lineage for each reconciliation.
func RequestMetadata(operation requestmeta.Operation) (Middleware, error) {
	if !operation.Valid() {
		return nil, invalid("valid controller operation is required", "invalid_controller_operation")
	}
	return func(next reconcile.Reconciler) reconcile.Reconciler {
		return ReconcilerFunc(func(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
			var err error
			ctx, _, err = requestmeta.EnsureRequestID(ctx)
			if err != nil {
				return reconcile.Result{}, err
			}
			ctx, err = requestmeta.WithOperation(ctx, operation)
			if err != nil {
				return reconcile.Result{}, err
			}
			return next.Reconcile(ctx, request)
		})
	}, nil
}
