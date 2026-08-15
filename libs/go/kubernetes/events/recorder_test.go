// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package events

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"go.mindclade.dev/libs/go/faults"
	kmetadata "go.mindclade.dev/libs/go/kubernetes/metadata"
	"go.mindclade.dev/libs/go/requestmeta"
)

type recordedEvent struct {
	object      runtime.Object
	annotations map[string]string
	eventType   string
	reason      string
	message     string
}

type fakeRecorder struct {
	events []recordedEvent
	panic  bool
}

func (recorder *fakeRecorder) Event(object runtime.Object, eventType, reason, message string) {
	recorder.record(object, nil, eventType, reason, "%s", message)
}

func (recorder *fakeRecorder) Eventf(object runtime.Object, eventType, reason, format string, arguments ...interface{}) {
	recorder.record(object, nil, eventType, reason, format, arguments...)
}

func (recorder *fakeRecorder) AnnotatedEventf(object runtime.Object, annotations map[string]string, eventType, reason, format string, arguments ...interface{}) {
	recorder.record(object, annotations, eventType, reason, format, arguments...)
}

func (recorder *fakeRecorder) record(object runtime.Object, annotations map[string]string, eventType, reason, format string, arguments ...interface{}) {
	if recorder.panic {
		panic("recorder unavailable")
	}
	recorder.events = append(recorder.events, recordedEvent{
		object:      object,
		annotations: kmetadata.Clone(annotations),
		eventType:   eventType,
		reason:      reason,
		message:     fmt.Sprintf(format, arguments...),
	})
}

func TestReasonValidation(t *testing.T) {
	reason, err := ParseReason(" Reconciled ")
	if err != nil || reason.String() != "Reconciled" || !reason.Valid() {
		t.Fatalf("ParseReason() = (%q, %v)", reason, err)
	}
	for _, value := range []string{"", "not-valid", "lowercase", string(make([]byte, 129))} {
		if _, err := ParseReason(value); err == nil {
			t.Fatalf("ParseReason(%q) returned nil", value)
		}
	}
}

func TestEventValidation(t *testing.T) {
	event := Event{Type: TypeNormal, Reason: MustParseReason("Reconciled"), Message: "resource reconciled"}
	if err := event.Validate(); err != nil {
		t.Fatal(err)
	}
	invalid := []Event{
		{Type: "Other", Reason: event.Reason, Message: event.Message},
		{Type: TypeNormal, Reason: "bad-reason", Message: event.Message},
		{Type: TypeNormal, Reason: event.Reason},
		{Type: TypeNormal, Reason: event.Reason, Message: string(make([]byte, MaximumMessageBytes+1))},
	}
	for index, candidate := range invalid {
		if err := candidate.Validate(); !faults.IsCode(err, faults.CodeInvalidArgument) {
			t.Fatalf("invalid event %d = %v", index, err)
		}
	}
}

func TestRecordAddsRequestLineageAndClonesAnnotations(t *testing.T) {
	provider := &fakeRecorder{}
	recorder, err := New(provider)
	if err != nil {
		t.Fatal(err)
	}
	requestID, err := requestmeta.NewRequestIDAt(time.Unix(100, 0))
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := requestmeta.New(requestID)
	if err != nil {
		t.Fatal(err)
	}
	ctx, err := requestmeta.WithMetadata(context.Background(), metadata)
	if err != nil {
		t.Fatal(err)
	}
	annotations := map[string]string{"mindclade.dev/source": "controller"}
	object := &metav1.PartialObjectMetadata{}
	err = recorder.Record(ctx, object, Event{
		Type:        TypeNormal,
		Reason:      MustParseReason("Reconciled"),
		Message:     "resource reconciled",
		Annotations: annotations,
	})
	if err != nil {
		t.Fatal(err)
	}
	annotations["mindclade.dev/source"] = "mutated"
	if len(provider.events) != 1 {
		t.Fatalf("events = %d", len(provider.events))
	}
	recorded := provider.events[0]
	if recorded.object != object || recorded.eventType != "Normal" || recorded.reason != "Reconciled" || recorded.message != "resource reconciled" {
		t.Fatalf("recorded = %#v", recorded)
	}
	if recorded.annotations["mindclade.dev/source"] != "controller" || recorded.annotations[kmetadata.RequestIDAnnotation] != requestID.String() {
		t.Fatalf("annotations = %#v", recorded.annotations)
	}
}

func TestRecordFaultUsesOnlyPublicMessage(t *testing.T) {
	provider := &fakeRecorder{}
	recorder, err := New(provider)
	if err != nil {
		t.Fatal(err)
	}
	secret := errors.New("database password=secret")
	failure := faults.Wrap(secret, faults.CodeUnavailable, "model registry is unavailable")
	if err := recorder.RecordFault(context.Background(), &metav1.PartialObjectMetadata{}, MustParseReason("RegistryUnavailable"), failure); err != nil {
		t.Fatal(err)
	}
	if got := provider.events[0].message; got != "model registry is unavailable" {
		t.Fatalf("message = %q", got)
	}
}

func TestRecorderContainsProviderPanic(t *testing.T) {
	recorder, err := New(&fakeRecorder{panic: true})
	if err != nil {
		t.Fatal(err)
	}
	err = recorder.Record(context.Background(), &metav1.PartialObjectMetadata{}, Event{Type: TypeWarning, Reason: MustParseReason("Failed"), Message: "failed"})
	if !faults.IsCode(err, faults.CodeInternal) || faults.ReasonOf(err) != "event_recorder_panicked" {
		t.Fatalf("Record() = %v", err)
	}
}

func TestRecorderRejectsInvalidInputs(t *testing.T) {
	if _, err := New(nil); !faults.IsCode(err, faults.CodeInvalidArgument) {
		t.Fatalf("New(nil) = %v", err)
	}
	recorder, err := New(&fakeRecorder{})
	if err != nil {
		t.Fatal(err)
	}
	event := Event{Type: TypeNormal, Reason: MustParseReason("Valid"), Message: "valid"}
	if err := recorder.Record(nil, &metav1.PartialObjectMetadata{}, event); !faults.IsCode(err, faults.CodeInvalidArgument) {
		t.Fatalf("Record(nil context) = %v", err)
	}
	if err := recorder.Record(context.Background(), nil, event); !faults.IsCode(err, faults.CodeInvalidArgument) {
		t.Fatalf("Record(nil object) = %v", err)
	}
}
