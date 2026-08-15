// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package servicekit

import (
	"context"
	"errors"
	"time"

	"mindclade.internal/libs/go/faults"
)

// Stable field names used by lifecycle faults and events.
const (
	FieldServiceName      = "service_name"
	FieldComponentName    = "component_name"
	FieldLifecyclePhase   = "lifecycle_phase"
	FieldProbeName        = "probe_name"
	FieldServiceState     = "service_state"
	FieldTimeout          = "timeout"
	FieldEventKind        = "event_kind"
	FieldFailedProbeNames = "failed_probe_names"
)

const (
	operationNew           = "servicekit.New"
	operationAdd           = "servicekit.Service.Add"
	operationRun           = "servicekit.Service.Run"
	operationWait          = "servicekit.Service.Wait"
	operationShutdown      = "servicekit.Service.Shutdown"
	operationSignalContext = "servicekit.SignalContext"
	operationProbeSetNew   = "servicekit.NewProbeSet"
	operationProbeRegister = "servicekit.ProbeSet.Register"
	operationProbeReplace  = "servicekit.ProbeSet.Replace"
	operationProbeCheck    = "servicekit.ProbeSet.Check"
)

func structuredFault(
	ctx context.Context,
	cause error,
	code faults.Code,
	message string,
	reason string,
	operation string,
	fields faults.Fields,
) error {
	options := make([]faults.Option, 0, 6)
	if reason != "" {
		options = append(options, faults.WithReason(reason))
	}
	if operation != "" {
		options = append(options, faults.WithOperation(operation))
	}
	if len(fields) > 0 {
		options = append(options, faults.WithFields(fields))
	}
	if ctx != nil {
		options = append(options, faults.WithContextMetadata(ctx))
	}
	if policy := faults.RetryPolicyOf(cause); policy.Specified() {
		options = append(options, faults.WithRetryPolicy(policy))
	}
	if cause == nil {
		return faults.New(code, message, options...)
	}
	return faults.Wrap(cause, code, message, options...)
}

func invalidNameError(kind, name, operation string) error {
	return structuredFault(
		nil,
		ErrInvalidName,
		faults.CodeInvalidArgument,
		"invalid "+kind+" name",
		"invalid_"+kind+"_name",
		operation,
		faults.Fields{"name_kind": kind, "name": name},
	)
}

func invalidDurationError(name string, value time.Duration) error {
	return structuredFault(
		nil,
		ErrInvalidDuration,
		faults.CodeInvalidArgument,
		"invalid service lifecycle duration",
		"invalid_duration",
		operationNew,
		faults.Fields{"duration_name": name, FieldTimeout: value.String()},
	)
}

func nilContextError(operation string) error {
	return structuredFault(
		nil,
		ErrNilContext,
		faults.CodeInvalidArgument,
		"context must not be nil",
		"nil_context",
		operation,
		nil,
	)
}

func nilServiceError(operation string) error {
	return structuredFault(
		nil,
		ErrNilService,
		faults.CodeInvalidArgument,
		"service must not be nil",
		"nil_service",
		operation,
		nil,
	)
}

func configurationFrozenError(service string) error {
	return structuredFault(
		nil,
		ErrConfigurationFrozen,
		faults.CodeFailedPrecondition,
		"service configuration is frozen",
		"service_configuration_frozen",
		operationAdd,
		faults.Fields{FieldServiceName: service},
	)
}

func alreadyRunError(service string) error {
	return structuredFault(
		nil,
		ErrAlreadyRun,
		faults.CodeFailedPrecondition,
		"service may only be run once",
		"service_already_run",
		operationRun,
		faults.Fields{FieldServiceName: service},
	)
}

func duplicateComponentError(service, component string) error {
	return structuredFault(
		nil,
		ErrDuplicateComponent,
		faults.CodeAlreadyExists,
		"service component is already registered",
		"duplicate_component",
		operationAdd,
		faults.Fields{FieldServiceName: service, FieldComponentName: component},
	)
}

func duplicateProbeError(name, operation string) error {
	return structuredFault(
		nil,
		ErrDuplicateProbe,
		faults.CodeAlreadyExists,
		"service health probe is already registered",
		"duplicate_probe",
		operation,
		faults.Fields{FieldProbeName: name},
	)
}

func nilComponentError(component string) error {
	return structuredFault(
		nil,
		ErrNilComponent,
		faults.CodeInvalidArgument,
		"service component has no lifecycle hooks or probes",
		"empty_component",
		operationAdd,
		faults.Fields{FieldComponentName: component},
	)
}

func nilProbeError(name, operation string) error {
	return structuredFault(
		nil,
		ErrNilProbe,
		faults.CodeInvalidArgument,
		"service health probe must not be nil",
		"nil_probe",
		operation,
		faults.Fields{FieldProbeName: name},
	)
}

func contextError(ctx context.Context, cause error, operation, service string) error {
	if cause == nil {
		return nil
	}
	code := faults.CodeOf(cause)
	if code == faults.CodeUnknown {
		if errors.Is(cause, context.DeadlineExceeded) {
			code = faults.CodeDeadlineExceeded
		} else {
			code = faults.CodeCanceled
		}
	}
	message := "service operation canceled"
	reason := "service_context_canceled"
	if code == faults.CodeDeadlineExceeded {
		message = "service operation deadline exceeded"
		reason = "service_context_deadline_exceeded"
	}
	return structuredFault(
		ctx,
		cause,
		code,
		message,
		reason,
		operation,
		faults.Fields{FieldServiceName: service},
	)
}

func startupTimeoutError(ctx context.Context, service string, cause error, timeout time.Duration) error {
	combined := errors.Join(ErrStartupTimeout, cause)
	return structuredFault(
		ctx,
		combined,
		faults.CodeDeadlineExceeded,
		"service startup timed out",
		"startup_timeout",
		operationRun,
		faults.Fields{FieldServiceName: service, FieldTimeout: timeout.String()},
	)
}

func shutdownTimeoutError(ctx context.Context, service string, cause error, timeout time.Duration) error {
	combined := errors.Join(ErrShutdownTimeout, cause)
	return structuredFault(
		ctx,
		combined,
		faults.CodeDeadlineExceeded,
		"service shutdown timed out",
		"shutdown_timeout",
		operationShutdown,
		faults.Fields{FieldServiceName: service, FieldTimeout: timeout.String()},
	)
}

func componentFailure(
	ctx context.Context,
	service string,
	component string,
	phase Phase,
	cause error,
) error {
	if cause == nil {
		return nil
	}
	attributed := &ComponentError{
		Service:   service,
		Component: component,
		Phase:     phase,
		Err:       cause,
	}
	return structuredFault(
		ctx,
		attributed,
		attributed.Code(),
		attributed.Message(),
		attributed.Reason(),
		attributed.Operation(),
		attributed.Fields(),
	)
}

func probeFailure(ctx context.Context, name string, cause error) error {
	if cause == nil {
		return nil
	}
	code := faults.CodeOf(cause)
	if code == faults.CodeUnknown {
		code = faults.CodeUnavailable
	}
	message := "service health probe failed"
	reason := "probe_failed"
	var panicErr *PanicError
	switch {
	case errors.As(cause, &panicErr):
		code = faults.CodeInternal
		reason = "probe_panicked"
	case errors.Is(cause, context.DeadlineExceeded):
		code = faults.CodeDeadlineExceeded
		message = "service health probe timed out"
		reason = "probe_timeout"
	case errors.Is(cause, context.Canceled):
		code = faults.CodeCanceled
		message = "service health probe canceled"
		reason = "probe_canceled"
	}
	return structuredFault(
		ctx,
		cause,
		code,
		message,
		reason,
		operationProbeCheck,
		faults.Fields{FieldProbeName: name},
	)
}

func stateFailure(probe string, state State) error {
	err := &StateError{Probe: probe, State: state}
	return structuredFault(
		nil,
		err,
		err.Code(),
		err.Message(),
		err.Reason(),
		operationProbeCheck,
		err.Fields(),
	)
}

func componentOperation(phase Phase) string {
	return "servicekit.component." + string(phase)
}
