// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package events

import (
	"context"
	"fmt"
	"reflect"
	"regexp"
	"strings"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"

	"mindclade.internal/libs/go/faults"
	"mindclade.internal/libs/go/kubernetes"
	kmetadata "mindclade.internal/libs/go/kubernetes/metadata"
	"mindclade.internal/libs/go/requestmeta"
)

const MaximumMessageBytes = 1024

var reasonPattern = regexp.MustCompile(`^[A-Z][A-Za-z0-9]*$`)

type Type string

const (
	TypeNormal  Type = "Normal"
	TypeWarning Type = "Warning"
)

func (eventType Type) Valid() bool { return eventType == TypeNormal || eventType == TypeWarning }

type Reason string

func ParseReason(value string) (Reason, error) {
	value = strings.TrimSpace(value)
	if len(value) > 128 || !reasonPattern.MatchString(value) {
		return "", invalid("invalid Kubernetes event reason", "invalid_event_reason", nil)
	}
	return Reason(value), nil
}

func MustParseReason(value string) Reason {
	reason, err := ParseReason(value)
	if err != nil {
		panic(err)
	}
	return reason
}

func (reason Reason) String() string { return string(reason) }
func (reason Reason) Valid() bool {
	_, err := ParseReason(string(reason))
	return err == nil
}

// Event is a validated Kubernetes event request.
type Event struct {
	Type        Type
	Reason      Reason
	Message     string
	Annotations map[string]string
}

func (event Event) Validate() error {
	if !event.Type.Valid() {
		return invalid("invalid Kubernetes event type", "invalid_event_type", faults.Fields{"event_type": string(event.Type)})
	}
	if !event.Reason.Valid() {
		return invalid("invalid Kubernetes event reason", "invalid_event_reason", nil)
	}
	if strings.TrimSpace(event.Message) == "" || len(event.Message) > MaximumMessageBytes {
		return invalid("invalid Kubernetes event message", "invalid_event_message", faults.Fields{"maximum_bytes": MaximumMessageBytes})
	}
	if err := kmetadata.ValidateAnnotations(event.Annotations); err != nil {
		return err
	}
	return nil
}

// Recorder is a panic-isolating adapter over client-go's EventRecorder.
type Recorder struct{ recorder record.EventRecorder }

func New(recorder record.EventRecorder) (*Recorder, error) {
	if nilInterface(recorder) {
		return nil, invalid("Kubernetes event recorder is required", "nil_event_recorder", nil)
	}
	return &Recorder{recorder: recorder}, nil
}

func (recorder *Recorder) Record(ctx context.Context, object runtime.Object, event Event) (err error) {
	if ctx == nil || recorder == nil || nilInterface(recorder.recorder) || nilInterface(object) {
		return invalid("invalid Kubernetes event request", "invalid_event_request", nil)
	}
	if err := event.Validate(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return kubernetes.Qualify(ctx, err, "kubernetes.events.Record", nil)
	}
	annotations := kmetadata.Clone(event.Annotations)
	if metadata, ok := requestmeta.FromContext(ctx); ok {
		requestAnnotations, metadataErr := kmetadata.RequestAnnotations(metadata)
		if metadataErr != nil {
			return metadataErr
		}
		annotations = kmetadata.Merge(annotations, requestAnnotations)
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			err = faults.New(
				faults.CodeInternal,
				"Kubernetes event recording failed",
				faults.WithCause(fmt.Errorf("event recorder panic: %v", recovered)),
				faults.WithReason("event_recorder_panicked"),
				faults.WithOperation("kubernetes.events.Record"),
				faults.WithContextMetadata(ctx),
				faults.WithRetryPolicy(faults.NoRetry()),
			)
		}
	}()
	recorder.recorder.AnnotatedEventf(object, annotations, string(event.Type), event.Reason.String(), "%s", event.Message)
	return nil
}

// RecordFault emits a warning event containing only the public fault message.
func (recorder *Recorder) RecordFault(ctx context.Context, object runtime.Object, reason Reason, err error) error {
	message := faults.PublicMessageOf(err)
	if strings.TrimSpace(message) == "" {
		message = "operation failed"
	}
	return recorder.Record(ctx, object, Event{Type: TypeWarning, Reason: reason, Message: message})
}

func invalid(message, reason string, fields faults.Fields) error {
	return faults.New(faults.CodeInvalidArgument, message, faults.WithReason(reason), faults.WithOperation("kubernetes.events"), faults.WithFields(fields), faults.WithRetryPolicy(faults.NoRetry()))
}

func nilInterface(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}
