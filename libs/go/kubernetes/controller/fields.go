// Copyright 2026 Mindclade. All rights reserved.
// Confidential and proprietary.

package controller

import (
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"mindclade.internal/libs/go/faults"
)

func requestFields(request reconcile.Request) faults.Fields {
	fields := faults.Fields{"name": request.Name}
	if request.Namespace != "" {
		fields["namespace"] = request.Namespace
	}
	return fields
}
