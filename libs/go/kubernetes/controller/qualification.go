// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package controller

import (
	"context"
	"strings"

	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"mindclade.internal/libs/go/kubernetes"
)

// Qualify converts Kubernetes API errors returned by the reconciler into
// Mindclade faults. Existing structured faults are preserved.
func Qualify(operation string) Middleware {
	operation = strings.TrimSpace(operation)
	if operation == "" {
		operation = "kubernetes.controller.Reconcile"
	}
	return func(next reconcile.Reconciler) reconcile.Reconciler {
		return ReconcilerFunc(func(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
			result, err := next.Reconcile(ctx, request)
			return result, kubernetes.Qualify(ctx, err, operation, requestFields(request))
		})
	}
}
