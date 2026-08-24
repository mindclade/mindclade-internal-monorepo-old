// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package controller

import (
	"context"

	"go.mindclade.dev/libs/go/coordination/workqueue"
	"go.mindclade.dev/libs/go/faults"
)

// refuseStageReconcile is the default stage handler.
//
// Stage reconciliation is domain code -- it reads the run's durable state,
// decides the next transition, and launches or cancels a workload -- and a
// composition root does not author it. Until a handler is injected the worker
// fails every item closed: the queue retries it against its attempt bound and
// then dead-letters it, which leaves the work visible to an operator.
// Acknowledging an item this process cannot reconcile would lose it silently,
// and a stage lost this way is a run that never finishes and never fails.
func refuseStageReconcile(context.Context, workqueue.Item) (workqueue.Result, error) {
	return workqueue.Result{}, faults.New(
		faults.CodeNotImplemented,
		"stage reconciler is not configured",
		faults.WithReason("stage_reconciler_not_configured"),
		faults.WithOperation("controlplane.controller.refuseStageReconcile"),
		faults.WithRetryPolicy(faults.NoRetry()),
	)
}
