// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package controller

import (
	"context"
	"strings"
	"time"

	"go.mindclade.dev/libs/go/coordination/leadership"
	"go.mindclade.dev/libs/go/coordination/workqueue"
	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/servicekit"
)

// stageLeaderJoinTimeout bounds the wait for the leader's tasks to unwind after
// the first of them stops. It is a shutdown budget, not a drain budget: the
// leadership context is already cancelled by the time it is used, so anything
// still running here is a task ignoring cancellation, and blocking forever on
// one would hold the lease a standby is waiting for.
const stageLeaderJoinTimeout = 10 * time.Second

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

// gateLeaderWork transfers ownership of several components' Run loops to one
// leadership handler.
//
// leadership.Elector takes exactly one handler, and this role now has two
// things that may only run while leadership is held: the controller-runtime
// manager, whose informer cache and reconcilers must not run on a standby, and
// the stage worker, which claims durable items other replicas must not claim.
// Gating them separately is not expressible, and running one of them ungated
// would put two processes on the same work.
//
// Every component comes back with Run removed, exactly as leadership.GateComponent
// leaves it, so each still registers at its canonical servicekit stage with its
// health hooks intact and no independent way to start. They come back keyed by
// name rather than in argument order: every gated component has the same
// servicekit.Component type, so a positional result let a caller swap two
// arguments and register the manager under the stage worker's key -- which
// compiles, and which the standby-run-loop tests cannot catch because both
// components are stripped either way. A duplicate name is refused here rather
// than at the first leadership acquisition, where servicekit.TaskGroup.Add
// would have found it.
//
// The group fails as a unit on purpose. If the manager stops, the stage worker
// is reconciling against a cache nothing is refreshing; if the worker stops,
// the manager is watching objects nothing will act on. Either way the honest
// answer is to surrender the lease and let a standby take the whole role.
func gateLeaderWork(name string, components ...servicekit.Component) (leadership.Handler, map[string]servicekit.Component, error) {
	const operation = "controlplane.controller.gateLeaderWork"
	if strings.TrimSpace(name) != name || name == "" || len(components) == 0 {
		return nil, nil, leaderWorkFault(faults.CodeInvalidArgument, "invalid_leader_work_group", "leader work requires a name and at least one component", operation)
	}
	type namedRun struct {
		name string
		run  servicekit.Hook
	}
	runs := make([]namedRun, 0, len(components))
	gated := make(map[string]servicekit.Component, len(components))
	for _, component := range components {
		if strings.TrimSpace(component.Name) == "" || component.Run == nil {
			return nil, nil, leaderWorkFault(faults.CodeInvalidArgument, "invalid_leader_managed_component",
				"leader-managed component requires a name and run function", operation)
		}
		if _, exists := gated[component.Name]; exists {
			return nil, nil, leaderWorkFault(faults.CodeInvalidArgument, "duplicate_leader_managed_component",
				"leader work names a component twice", operation)
		}
		runs = append(runs, namedRun{name: component.Name, run: component.Run})
		component.Run = nil
		gated[component.Name] = component
	}
	handler := func(ctx context.Context, _ leadership.Session) error {
		group, err := servicekit.NewTaskGroup(name, nil)
		if err != nil {
			return err
		}
		for _, entry := range runs {
			if err := group.Add(entry.name, servicekit.Task(entry.run)); err != nil {
				return err
			}
		}
		if err := group.Start(ctx); err != nil {
			return err
		}
		first := group.WaitFirst(ctx)
		group.Cancel(first)
		// Detached from ctx: ctx is already cancelled on every path that
		// reaches here, so joining on it would return immediately and leave
		// the tasks running while the elector released the lease.
		joinCtx, cancelJoin := context.WithTimeout(context.Background(), stageLeaderJoinTimeout)
		defer cancelJoin()
		if _, joinErr := group.Join(joinCtx); joinErr != nil {
			return joinErr
		}
		if ctx.Err() != nil {
			return nil
		}
		if first != nil {
			return first
		}
		// A leader loop that returns nil while leadership is still held has
		// stopped doing the work the lease exists to serialize. Reporting it as
		// graceful completion would leave the process holding a lease and
		// reconciling nothing.
		return leaderWorkFault(faults.CodeUnavailable, "leader_work_stopped", "leader work stopped unexpectedly", operation)
	}
	return handler, gated, nil
}

func leaderWorkFault(code faults.Code, reason, message, operation string) error {
	return faults.New(code, message,
		faults.WithReason(reason),
		faults.WithOperation(operation),
		faults.WithRetryPolicy(faults.NoRetry()),
	)
}
