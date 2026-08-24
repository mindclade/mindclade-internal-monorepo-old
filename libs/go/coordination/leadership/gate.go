// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package leadership

import (
	"context"
	"strings"
	"time"

	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/servicekit"
)

// leaderWorkJoinTimeout bounds the wait for the leader's tasks to unwind after
// the first of them stops. It is a shutdown budget, not a drain budget: the
// leadership context is already cancelled by the time it is used, so anything
// still running here is a task ignoring cancellation, and blocking forever on
// one would hold the lease a standby is waiting for.
const leaderWorkJoinTimeout = 10 * time.Second

// GateComponents is GateComponent for N components that must lead together.
//
// An Elector takes exactly one handler, so a role with two things that may only
// run while leadership is held -- a controller-runtime manager whose informer
// cache must not run on a standby, and a workqueue worker that claims durable
// items other replicas must not claim -- cannot express that with GateComponent
// alone. Gating them separately is not possible, and running one of them
// ungated puts two processes on the same work.
//
// Every component comes back with Run removed, exactly as GateComponent leaves
// it, so each still registers at its canonical servicekit stage with its health
// hooks intact and no independent way to start. Per-component validation is
// GateComponent's, called here rather than repeated, which is why an
// unnameable or unrunnable component is refused with GateComponent's own
// reason and operation: one gate, one rule, nothing to drift.
//
// They come back keyed by name rather than in argument order. Every gated
// component has the same servicekit.Component type, so a positional result let
// a caller swap two arguments and register one component under another's key --
// which compiles, and which a standby-run-loop test cannot catch because both
// components are stripped either way. A duplicate name is refused here rather
// than at the first leadership acquisition, where servicekit.TaskGroup.Add
// would have found it and the failure would be a lost lease instead of a
// refused composition.
//
// The group fails as a unit on purpose. If one member stops, the others are
// running against something nothing is maintaining, and the honest answer is to
// surrender the lease and let a standby take the whole role.
//
// The handler ignores the Session it is given: a component that must write
// under the fence reads a live SessionView, which GateComponentWithSession
// binds, because a fence copied once at acquisition is stale one RenewInterval
// later.
func GateComponents(name string, components ...servicekit.Component) (Handler, map[string]servicekit.Component, error) {
	const operation = "leadership.GateComponents"
	if strings.TrimSpace(name) != name || name == "" || len(components) == 0 {
		return nil, nil, invalid("invalid_leader_work_group",
			"leader work requires a name and at least one component", operation)
	}
	type namedRun struct {
		name string
		run  Handler
	}
	runs := make([]namedRun, 0, len(components))
	gated := make(map[string]servicekit.Component, len(components))
	for _, component := range components {
		run, stripped, err := GateComponent(component)
		if err != nil {
			return nil, nil, err
		}
		if _, exists := gated[stripped.Name]; exists {
			return nil, nil, invalid("duplicate_leader_managed_component",
				"leader work names a component twice", operation)
		}
		runs = append(runs, namedRun{name: stripped.Name, run: run})
		gated[stripped.Name] = stripped
	}
	handler := func(ctx context.Context, session Session) error {
		group, err := servicekit.NewTaskGroup(name, nil)
		if err != nil {
			return err
		}
		for _, entry := range runs {
			run := entry.run
			if err := group.Add(entry.name, servicekit.Task(func(taskCtx context.Context) error {
				return run(taskCtx, session)
			})); err != nil {
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
		joinCtx, cancelJoin := context.WithTimeout(context.Background(), leaderWorkJoinTimeout)
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
		return faults.New(faults.CodeUnavailable, "leader work stopped unexpectedly",
			faults.WithReason("leader_work_stopped"),
			faults.WithOperation(operation),
			faults.WithRetryPolicy(faults.NoRetry()),
		)
	}
	return handler, gated, nil
}
