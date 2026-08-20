// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package observability

import (
	"context"
	"errors"

	"go.mindclade.dev/libs/go/faults"
)

var (
	ErrInvalidAttributes   = errors.New("observability: invalid attributes")
	ErrInvalidLabels       = errors.New("observability: invalid metric labels")
	ErrInvalidResource     = errors.New("observability: invalid resource")
	ErrInvalidTraceContext = errors.New("observability: invalid trace context")
	ErrInvalidMetric       = errors.New("observability: invalid metric")
	ErrInvalidComponent    = errors.New("observability: invalid lifecycle component")
	ErrDuplicateComponent  = errors.New("observability: duplicate lifecycle component")
	ErrPipelineClosed      = errors.New("observability: lifecycle pipeline closed")
	ErrNilContext          = errors.New("observability: nil context")
	ErrNilCarrier          = errors.New("observability: nil carrier")
	ErrNilHandler          = errors.New("observability: nil slog handler")
	ErrNilRuntime          = errors.New("observability: nil runtime")
	ErrProviderPanic       = errors.New("observability: provider panic")
)

const (
	operationNewRuntime    = "observability.NewRuntime"
	operationNewResource   = "observability.NewResource"
	operationNewAttributes = "observability.NewAttributes"
	operationNewLabels     = "observability.NewLabels"
	operationNewMetrics    = "observability.NewMetrics"
	operationRecordMetric  = "observability.Metrics.Record"
	operationExtract       = "observability.Propagator.Extract"
	operationInject        = "observability.Propagator.Inject"
	operationPipelineAdd   = "observability.Pipeline.Add"
	operationPipelineFlush = "observability.Pipeline.ForceFlush"
	operationPipelineClose = "observability.Pipeline.Shutdown"
)

func newFault(ctx context.Context, cause error, code faults.Code, message, reason, operation string, fields faults.Fields) error {
	options := []faults.Option{
		faults.WithReason(reason),
		faults.WithOperation(operation),
		faults.WithRetryPolicy(faults.NoRetry()),
	}
	if len(fields) > 0 {
		options = append(options, faults.WithFields(fields))
	}
	if ctx != nil {
		options = append(options, faults.WithContextMetadata(ctx))
	}
	if cause == nil {
		return faults.New(code, message, options...)
	}
	return faults.Wrap(cause, code, message, options...)
}

func invalidArgument(cause error, message, reason, operation string, fields faults.Fields) error {
	return newFault(nil, cause, faults.CodeInvalidArgument, message, reason, operation, fields)
}

// ErrorHandler receives failures from best-effort telemetry extensions.
// Handlers must be safe for concurrent use and should return promptly.
type ErrorHandler interface {
	Handle(error)
}

// ErrorHandlerFunc adapts a function to ErrorHandler.
type ErrorHandlerFunc func(error)

func (function ErrorHandlerFunc) Handle(err error) {
	if function != nil && err != nil {
		function(err)
	}
}

type nopErrorHandler struct{}

func (nopErrorHandler) Handle(error) {}

func safeHandle(handler ErrorHandler, err error) {
	if err == nil || nilInterface(handler) {
		return
	}
	defer func() { _ = recover() }()
	handler.Handle(err)
}
