// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package servicekit

import (
	"context"
	"fmt"
	"time"

	"go.mindclade.dev/libs/go/faults"
)

// Task is one owned long-running unit. It must honor cancellation and must not
// spawn detached work outside its owning TaskGroup.
type Task func(context.Context) error

type TaskState uint8

const (
	TaskRegistered TaskState = iota
	TaskRunning
	TaskSucceeded
	TaskFailed
	TaskCanceled
)

func (state TaskState) String() string {
	switch state {
	case TaskRegistered:
		return "registered"
	case TaskRunning:
		return "running"
	case TaskSucceeded:
		return "succeeded"
	case TaskFailed:
		return "failed"
	case TaskCanceled:
		return "canceled"
	default:
		return "unknown"
	}
}
func (state TaskState) Terminal() bool {
	return state == TaskSucceeded || state == TaskFailed || state == TaskCanceled
}

type TaskSnapshot struct {
	Name       string
	State      TaskState
	StartedAt  time.Time
	FinishedAt time.Time
	Err        error
}

type TaskError struct {
	Group string
	Task  string
	Err   error
}

func (err *TaskError) Error() string {
	if err == nil {
		return "<nil>"
	}
	return fmt.Sprintf("servicekit: task group %q task %q: %v", err.Group, err.Task, err.Err)
}
func (err *TaskError) Unwrap() error {
	if err == nil {
		return nil
	}
	return err.Err
}
func (err *TaskError) Code() faults.Code {
	code := faults.CodeOf(err.Err)
	if code == faults.CodeUnknown {
		return faults.CodeUnavailable
	}
	return code
}
func (err *TaskError) Message() string { return "owned service task failed" }
func (err *TaskError) Reason() string {
	reason := faults.ReasonOf(err.Err)
	if reason != "" {
		return reason
	}
	return "owned_task_failed"
}
func (err *TaskError) Fields() faults.Fields {
	return faults.FieldsOf(err.Err).Merge(faults.Fields{"task_group": err.Group, "task_name": err.Task})
}
func (err *TaskError) RetryPolicy() faults.RetryPolicy { return faults.RetryPolicyOf(err.Err) }
