// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package servicekit

import (
	"errors"
	"fmt"

	"mindclade.internal/libs/go/faults"
)

// Sentinel errors provide stable errors.Is targets. Exported operations return
// structured faults that wrap these sentinels rather than returning them bare.
var (
	// ErrAlreadyRun is returned when Run is invoked more than once.
	ErrAlreadyRun = errors.New("servicekit: service may only be run once")

	// ErrConfigurationFrozen is returned when lifecycle configuration is
	// changed after the service has started.
	ErrConfigurationFrozen = errors.New("servicekit: service configuration is frozen")

	// ErrInvalidName identifies an invalid service, component, or probe name.
	ErrInvalidName = errors.New("servicekit: invalid name")

	// ErrInvalidDuration identifies a negative duration supplied to an option.
	ErrInvalidDuration = errors.New("servicekit: duration must not be negative")

	// ErrDuplicateComponent identifies a duplicate component registration.
	ErrDuplicateComponent = errors.New("servicekit: duplicate component")

	// ErrDuplicateProbe identifies a duplicate probe registration.
	ErrDuplicateProbe = errors.New("servicekit: duplicate probe")

	// ErrNilComponent identifies a component with no lifecycle hooks or probes.
	ErrNilComponent = errors.New("servicekit: component has no hooks or probes")

	// ErrNilProbe identifies a nil probe registration.
	ErrNilProbe = errors.New("servicekit: probe must not be nil")

	// ErrNilContext identifies a nil context.Context argument.
	ErrNilContext = errors.New("servicekit: context must not be nil")

	// ErrNilClock identifies a nil or typed-nil clock implementation.
	ErrNilClock = errors.New("servicekit: clock must not be nil")

	// ErrNilService identifies an operation invoked on a nil *Service.
	ErrNilService = errors.New("servicekit: service must not be nil")

	// ErrStartupTimeout identifies exhaustion of the startup budget.
	ErrStartupTimeout = errors.New("servicekit: startup timeout")

	// ErrShutdownTimeout identifies exhaustion of the shutdown budget.
	ErrShutdownTimeout = errors.New("servicekit: shutdown timeout")

	// ErrTaskGroupStarted identifies a second task-group start.
	ErrTaskGroupStarted = errors.New("servicekit: task group already started")

	// ErrTaskGroupNotStarted identifies task operations before Start.
	ErrTaskGroupNotStarted = errors.New("servicekit: task group not started")

	// ErrDuplicateTask identifies a duplicate named task.
	ErrDuplicateTask = errors.New("servicekit: duplicate task")

	// ErrNilTask identifies a missing task function.
	ErrNilTask = errors.New("servicekit: task must not be nil")

	// ErrEmptyTaskGroup identifies a group with no registered tasks.
	ErrEmptyTaskGroup = errors.New("servicekit: task group is empty")
)

var errShutdownRequested = errors.New("servicekit: shutdown requested")

// Phase identifies a component lifecycle phase.
type Phase string

const (
	PhaseStart Phase = "start"
	PhaseRun   Phase = "run"
	PhaseDrain Phase = "drain"
	PhaseStop  Phase = "stop"
	PhaseProbe Phase = "probe"
)

// ComponentError attributes a failure to a service component and lifecycle
// phase. Servicekit normally wraps this value in a faults.Fault; the provider
// methods below also make a directly constructed ComponentError classifiable.
type ComponentError struct {
	Service   string
	Component string
	Phase     Phase
	Err       error
}

var _ error = (*ComponentError)(nil)

// Error implements error.
func (err *ComponentError) Error() string {
	if err == nil {
		return "<nil>"
	}
	prefix := "servicekit"
	if err.Service != "" {
		prefix += ": service " + fmt.Sprintf("%q", err.Service)
	}
	if err.Err == nil {
		return fmt.Sprintf("%s: component %q %s failed", prefix, err.Component, err.Phase)
	}
	return fmt.Sprintf("%s: component %q %s: %v", prefix, err.Component, err.Phase, err.Err)
}

// Unwrap returns the underlying component error.
func (err *ComponentError) Unwrap() error {
	if err == nil {
		return nil
	}
	return err.Err
}

// Code provides a transport-neutral classification to faults.CodeOf.
func (err *ComponentError) Code() faults.Code {
	if err == nil {
		return faults.CodeUnknown
	}
	if code := faults.CodeOf(err.Err); code != faults.CodeUnknown {
		return code
	}
	var panicErr *PanicError
	if errors.As(err.Err, &panicErr) {
		return faults.CodeInternal
	}
	switch err.Phase {
	case PhaseStart, PhaseRun, PhaseDrain, PhaseProbe:
		return faults.CodeUnavailable
	case PhaseStop:
		return faults.CodeInternal
	default:
		return faults.CodeUnknown
	}
}

// Message returns a client-safe summary.
func (err *ComponentError) Message() string {
	if err == nil {
		return ""
	}
	switch err.Phase {
	case PhaseStart:
		return "service component failed to start"
	case PhaseRun:
		return "service component exited with an error"
	case PhaseDrain:
		return "service component failed to drain"
	case PhaseStop:
		return "service component failed to stop"
	case PhaseProbe:
		return "service component health check failed"
	default:
		return "service component failed"
	}
}

// Reason returns stable machine-readable detail.
func (err *ComponentError) Reason() string {
	if err == nil {
		return ""
	}
	if reason := faults.ReasonOf(err.Err); reason != "" {
		return reason
	}
	switch err.Phase {
	case PhaseStart:
		return "component_start_failed"
	case PhaseRun:
		return "component_run_failed"
	case PhaseDrain:
		return "component_drain_failed"
	case PhaseStop:
		return "component_stop_failed"
	case PhaseProbe:
		return "component_probe_failed"
	default:
		return "component_failed"
	}
}

// Operation returns the logical lifecycle operation.
func (err *ComponentError) Operation() string {
	if err == nil {
		return ""
	}
	if operation := faults.OperationOf(err.Err); operation != "" {
		return operation
	}
	return "servicekit.component." + string(err.Phase)
}

// Fields returns structured diagnostic metadata.
func (err *ComponentError) Fields() faults.Fields {
	if err == nil {
		return nil
	}
	return faults.FieldsOf(err.Err).Merge(faults.Fields{
		FieldServiceName:    err.Service,
		FieldComponentName:  err.Component,
		FieldLifecyclePhase: string(err.Phase),
	})
}

// RetryPolicy preserves explicit retry intent from the underlying failure.
func (err *ComponentError) RetryPolicy() faults.RetryPolicy {
	if err == nil {
		return faults.RetryPolicy{}
	}
	return faults.RetryPolicyOf(err.Err)
}

// StateError reports that a lifecycle probe was evaluated in an unacceptable
// service state.
type StateError struct {
	Probe string
	State State
}

var _ error = (*StateError)(nil)

// Error implements error.
func (err *StateError) Error() string {
	if err == nil {
		return "<nil>"
	}
	return fmt.Sprintf("servicekit: %s probe failed in %s state", err.Probe, err.State)
}

// Code provides a transport-neutral classification.
func (err *StateError) Code() faults.Code {
	return faults.CodeFailedPrecondition
}

// Message returns a client-safe summary.
func (err *StateError) Message() string {
	if err == nil {
		return ""
	}
	switch err.Probe {
	case "liveness":
		return "service is not live"
	case "readiness":
		return "service is not ready"
	default:
		return "service state does not satisfy the health check"
	}
}

// Reason returns stable machine-readable detail.
func (err *StateError) Reason() string {
	if err == nil {
		return ""
	}
	switch err.Probe {
	case "liveness":
		return "service_not_live"
	case "readiness":
		return "service_not_ready"
	default:
		return "service_state_rejected"
	}
}

// Fields returns structured diagnostic metadata.
func (err *StateError) Fields() faults.Fields {
	if err == nil {
		return nil
	}
	return faults.Fields{
		FieldProbeName:    err.Probe,
		FieldServiceState: err.State.String(),
	}.Clone()
}

// PanicError records a panic recovered at a component, probe, or observer
// boundary. Error intentionally excludes the captured stack trace.
type PanicError struct {
	Value any
	stack []byte
}

var _ error = (*PanicError)(nil)

// Error implements error.
func (err *PanicError) Error() string {
	if err == nil {
		return "<nil>"
	}
	return fmt.Sprintf("servicekit: panic recovered: %v", err.Value)
}

// Code classifies a recovered panic as an internal failure.
func (err *PanicError) Code() faults.Code {
	return faults.CodeInternal
}

// Message returns a client-safe summary that does not expose the panic value.
func (err *PanicError) Message() string {
	return "service lifecycle operation panicked"
}

// Reason returns stable machine-readable detail.
func (err *PanicError) Reason() string {
	return "panic_recovered"
}

// Stack returns a defensive copy of the captured stack trace.
func (err *PanicError) Stack() []byte {
	if err == nil || len(err.stack) == 0 {
		return nil
	}
	copied := make([]byte, len(err.stack))
	copy(copied, err.stack)
	return copied
}
