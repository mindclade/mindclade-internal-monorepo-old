// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

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
