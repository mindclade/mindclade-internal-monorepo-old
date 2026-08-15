// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package servicekit

import (
	"context"
	"errors"
	"sort"
	"sync"

	"go.mindclade.dev/libs/go/clock"
	"go.mindclade.dev/libs/go/faults"
)

type taskDefinition struct {
	name string
	task Task
}
type taskRecord struct{ snapshot TaskSnapshot }
type taskCompletion struct {
	name string
	err  error
}

// JoinReport is an immutable summary of all tasks known at Join time.
type JoinReport struct{ Tasks []TaskSnapshot }

func (report JoinReport) Failures() []TaskSnapshot {
	var values []TaskSnapshot
	for _, task := range report.Tasks {
		if task.State == TaskFailed {
			values = append(values, task)
		}
	}
	return values
}
func (report JoinReport) Complete() bool {
	for _, task := range report.Tasks {
		if !task.State.Terminal() {
			return false
		}
	}
	return true
}

// TaskGroup owns named goroutines, cancellation, panic containment, and joins.
// Tasks may be registered before Start with Add or after Start with Spawn.
type TaskGroup struct {
	name        string
	valueClock  clock.Clock
	mu          sync.RWMutex
	definitions []taskDefinition
	records     map[string]*taskRecord
	started     bool
	closed      bool
	ctx         context.Context
	cancel      context.CancelCauseFunc
	wait        sync.WaitGroup
	completion  chan taskCompletion
}

func NewTaskGroup(name string, valueClock clock.Clock) (*TaskGroup, error) {
	if err := validateName("task_group", name, "servicekit.NewTaskGroup"); err != nil {
		return nil, err
	}
	if valueClock == nil {
		valueClock = clock.RealClock{}
	}
	return &TaskGroup{name: name, valueClock: valueClock, records: make(map[string]*taskRecord), completion: make(chan taskCompletion, 1)}, nil
}

func (group *TaskGroup) Add(name string, task Task) error {
	if group == nil {
		return structuredFault(nil, ErrNilTask, faults.CodeInvalidArgument, "task group must not be nil", "nil_task_group", "servicekit.TaskGroup.Add", nil)
	}
	if err := validateName("task", name, "servicekit.TaskGroup.Add"); err != nil {
		return err
	}
	if task == nil {
		return structuredFault(nil, ErrNilTask, faults.CodeInvalidArgument, "task must not be nil", "nil_task", "servicekit.TaskGroup.Add", faults.Fields{"task_name": name})
	}
	group.mu.Lock()
	defer group.mu.Unlock()
	if group.started {
		return structuredFault(nil, ErrConfigurationFrozen, faults.CodeFailedPrecondition, "task group registration is frozen", "task_group_frozen", "servicekit.TaskGroup.Add", faults.Fields{"task_group": group.name})
	}
	if _, exists := group.records[name]; exists {
		return structuredFault(nil, ErrDuplicateTask, faults.CodeAlreadyExists, "task is already registered", "duplicate_task", "servicekit.TaskGroup.Add", faults.Fields{"task_group": group.name, "task_name": name})
	}
	group.definitions = append(group.definitions, taskDefinition{name: name, task: task})
	group.records[name] = &taskRecord{snapshot: TaskSnapshot{Name: name, State: TaskRegistered}}
	return nil
}

func (group *TaskGroup) Start(parent context.Context) error {
	if parent == nil {
		return nilContextError("servicekit.TaskGroup.Start")
	}
	if group == nil {
		return structuredFault(nil, ErrNilTask, faults.CodeInvalidArgument, "task group must not be nil", "nil_task_group", "servicekit.TaskGroup.Start", nil)
	}
	group.mu.Lock()
	if group.started {
		group.mu.Unlock()
		return structuredFault(nil, ErrTaskGroupStarted, faults.CodeFailedPrecondition, "task group already started", "task_group_already_started", "servicekit.TaskGroup.Start", faults.Fields{"task_group": group.name})
	}
	if len(group.definitions) == 0 {
		group.mu.Unlock()
		return structuredFault(nil, ErrEmptyTaskGroup, faults.CodeFailedPrecondition, "task group contains no tasks", "empty_task_group", "servicekit.TaskGroup.Start", faults.Fields{"task_group": group.name})
	}
	group.ctx, group.cancel = context.WithCancelCause(parent)
	group.started = true
	definitions := append([]taskDefinition(nil), group.definitions...)
	group.mu.Unlock()
	for _, definition := range definitions {
		group.launch(definition.name, definition.task)
	}
	return nil
}

func (group *TaskGroup) Spawn(name string, task Task) error {
	if group == nil {
		return structuredFault(nil, ErrNilTask, faults.CodeInvalidArgument, "task group must not be nil", "nil_task_group", "servicekit.TaskGroup.Spawn", nil)
	}
	if err := validateName("task", name, "servicekit.TaskGroup.Spawn"); err != nil {
		return err
	}
	if task == nil {
		return structuredFault(nil, ErrNilTask, faults.CodeInvalidArgument, "task must not be nil", "nil_task", "servicekit.TaskGroup.Spawn", faults.Fields{"task_name": name})
	}
	group.mu.Lock()
	if !group.started {
		group.mu.Unlock()
		return structuredFault(nil, ErrTaskGroupNotStarted, faults.CodeFailedPrecondition, "task group has not started", "task_group_not_started", "servicekit.TaskGroup.Spawn", faults.Fields{"task_group": group.name})
	}
	if group.closed {
		group.mu.Unlock()
		return structuredFault(nil, ErrConfigurationFrozen, faults.CodeFailedPrecondition, "task group is closing", "task_group_closing", "servicekit.TaskGroup.Spawn", faults.Fields{"task_group": group.name})
	}
	if _, exists := group.records[name]; exists {
		group.mu.Unlock()
		return structuredFault(nil, ErrDuplicateTask, faults.CodeAlreadyExists, "task is already registered", "duplicate_task", "servicekit.TaskGroup.Spawn", faults.Fields{"task_group": group.name, "task_name": name})
	}
	group.records[name] = &taskRecord{snapshot: TaskSnapshot{Name: name, State: TaskRegistered}}
	group.mu.Unlock()
	group.launch(name, task)
	return nil
}

func (group *TaskGroup) launch(name string, task Task) {
	group.wait.Add(1)
	go func() {
		defer group.wait.Done()
		startedAt := group.valueClock.Now()
		group.mu.Lock()
		record := group.records[name]
		record.snapshot.State = TaskRunning
		record.snapshot.StartedAt = startedAt
		group.mu.Unlock()
		err := invoke(group.ctx, task)
		finishedAt := group.valueClock.Now()
		state := TaskSucceeded
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				state = TaskCanceled
			} else {
				state = TaskFailed
				err = &TaskError{Group: group.name, Task: name, Err: err}
			}
		}
		group.mu.Lock()
		record.snapshot.State = state
		record.snapshot.FinishedAt = finishedAt
		record.snapshot.Err = err
		group.mu.Unlock()
		select {
		case group.completion <- taskCompletion{name: name, err: err}:
		default:
		}
	}()
}

// WaitFirst waits for the first task to finish or ctx cancellation.
func (group *TaskGroup) WaitFirst(ctx context.Context) error {
	if ctx == nil {
		return nilContextError("servicekit.TaskGroup.WaitFirst")
	}
	if group == nil {
		return ErrTaskGroupNotStarted
	}
	group.mu.RLock()
	started := group.started
	group.mu.RUnlock()
	if !started {
		return ErrTaskGroupNotStarted
	}
	select {
	case completed := <-group.completion:
		return completed.err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (group *TaskGroup) Cancel(cause error) {
	if group == nil {
		return
	}
	group.mu.Lock()
	group.closed = true
	cancel := group.cancel
	group.mu.Unlock()
	if cancel != nil {
		if cause == nil {
			cause = context.Canceled
		}
		cancel(cause)
	}
}

func (group *TaskGroup) Join(ctx context.Context) (JoinReport, error) {
	if ctx == nil {
		return JoinReport{}, nilContextError("servicekit.TaskGroup.Join")
	}
	if group == nil {
		return JoinReport{}, ErrTaskGroupNotStarted
	}
	done := make(chan struct{})
	go func() { group.wait.Wait(); close(done) }()
	select {
	case <-done:
		return group.Report(), nil
	case <-ctx.Done():
		return group.Report(), contextError(ctx, ctx.Err(), "servicekit.TaskGroup.Join", group.name)
	}
}

func (group *TaskGroup) Report() JoinReport {
	if group == nil {
		return JoinReport{}
	}
	group.mu.RLock()
	tasks := make([]TaskSnapshot, 0, len(group.records))
	for _, record := range group.records {
		tasks = append(tasks, record.snapshot)
	}
	group.mu.RUnlock()
	sort.Slice(tasks, func(left, right int) bool { return tasks[left].Name < tasks[right].Name })
	return JoinReport{Tasks: tasks}
}

// Component integrates the task group with servicekit. It starts tasks in the
// Run phase, cancels them during Drain, and verifies they have joined in Stop.
func (group *TaskGroup) Component(name string) Component {
	return Component{
		Name: name,
		Run: func(ctx context.Context) error {
			if err := group.Start(ctx); err != nil {
				return err
			}
			return group.WaitFirst(ctx)
		},
		Drain: func(ctx context.Context) error {
			group.Cancel(errShutdownRequested)
			_, err := group.Join(ctx)
			return err
		},
		Stop: func(ctx context.Context) error {
			group.Cancel(errShutdownRequested)
			_, err := group.Join(ctx)
			return err
		},
		Liveness: func(context.Context) error {
			for _, task := range group.Report().Tasks {
				if task.State == TaskFailed {
					return task.Err
				}
			}
			return nil
		},
	}
}
