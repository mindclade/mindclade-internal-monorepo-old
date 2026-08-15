// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package audit

import (
	"context"
	"errors"

	"mindclade.internal/libs/go/faults"
)

// Recorder persists or publishes one immutable event. Successful return means
// the implementation accepted durable responsibility according to its own
// documented delivery contract.
type Recorder interface {
	Record(context.Context, Event) error
}

type RecorderFunc func(context.Context, Event) error

func (function RecorderFunc) Record(ctx context.Context, event Event) error {
	return function(ctx, event)
}

// NopRecorder intentionally discards valid events and is useful only in tests
// and explicitly unaudited local tooling.
type NopRecorder struct{}

func (NopRecorder) Record(context.Context, Event) error { return nil }

// Record validates an event and qualifies recorder failures.
func Record(ctx context.Context, recorder Recorder, event Event) error {
	if ctx == nil {
		return faults.Wrap(ErrNilContext, faults.CodeInvalidArgument, "unable to record audit event with a nil context", faults.WithReason("nil_context"), faults.WithOperation("audit.Record"))
	}
	if nilInterface(recorder) {
		return faults.Wrap(ErrNilRecorder, faults.CodeFailedPrecondition, "audit recorder is not configured", faults.WithReason("audit_recorder_missing"), faults.WithOperation("audit.Record"))
	}
	if err := event.Validate(); err != nil {
		return err
	}
	if err := recorder.Record(ctx, event); err != nil {
		code := faults.CodeOf(err)
		if code == faults.CodeUnknown {
			code = faults.CodeUnavailable
		}
		reason := faults.ReasonOf(err)
		if reason == "" {
			reason = "audit_recorder_failed"
		}
		retry := faults.RetryPolicyOf(err)
		if !retry.Specified() && code == faults.CodeUnavailable {
			retry = faults.BackoffRetry(0)
		}
		return faults.Wrap(
			errors.Join(ErrRecorderFailure, err),
			code,
			"audit event could not be recorded",
			faults.WithReason(reason),
			faults.WithOperation("audit.Record"),
			faults.WithRetryPolicy(retry),
			faults.WithFields(faults.FieldsOf(err)),
			faults.WithField("audit_event_id", event.ID().String()),
			faults.WithContextMetadata(ctx),
		)
	}
	return nil
}
