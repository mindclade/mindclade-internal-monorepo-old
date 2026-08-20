// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package kubernetes

import (
	"context"
	"errors"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"go.mindclade.dev/libs/go/faults"
)

func TestQualifyNotFound(t *testing.T) {
	err := apierrors.NewNotFound(schema.GroupResource{Group: "mindclade.dev", Resource: "runs"}, "run-1")
	qualified := Qualify(context.Background(), err, "runs.Get", nil)
	if got := faults.CodeOf(qualified); got != faults.CodeNotFound {
		t.Fatalf("CodeOf() = %q, want %q", got, faults.CodeNotFound)
	}
	if got := faults.ReasonOf(qualified); got != "kubernetes_resource_not_found" {
		t.Fatalf("ReasonOf() = %q", got)
	}
	if got := faults.FieldsOf(qualified)[FieldName]; got != "run-1" {
		t.Fatalf("name field = %#v", got)
	}
	if !errors.Is(qualified, err) {
		t.Fatal("qualified error does not wrap provider error")
	}
}

func TestQualifyPreservesFault(t *testing.T) {
	original := faults.New(faults.CodeInvalidArgument, "invalid")
	if got := Qualify(context.Background(), original, "test", nil); got != original {
		t.Fatal("structured fault was replaced")
	}
}

func TestQualifyAPIStatusMatrix(t *testing.T) {
	tests := []struct {
		name   string
		reason metav1.StatusReason
		code   int32
		want   faults.Code
		retry  bool
	}{
		{"already exists", metav1.StatusReasonAlreadyExists, 409, faults.CodeAlreadyExists, false},
		{"conflict", metav1.StatusReasonConflict, 409, faults.CodeConflict, true},
		{"invalid", metav1.StatusReasonInvalid, 422, faults.CodeInvalidArgument, false},
		{"forbidden", metav1.StatusReasonForbidden, 403, faults.CodePermissionDenied, false},
		{"unauthorized", metav1.StatusReasonUnauthorized, 401, faults.CodeUnauthenticated, false},
		{"rate limited", metav1.StatusReasonTooManyRequests, 429, faults.CodeResourceExhausted, true},
		{"too large", metav1.StatusReasonRequestEntityTooLarge, 413, faults.CodeResourceExhausted, false},
		{"unavailable", metav1.StatusReasonServiceUnavailable, 503, faults.CodeUnavailable, true},
		// StatusReasonExpired, not StatusReasonResourceExpired — apimachinery has no constant
		// by the latter name. It is the 410 Gone reason, which is what this row already asserts.
		{"expired", metav1.StatusReasonExpired, 410, faults.CodeAborted, true},
		{"unsupported", metav1.StatusReasonMethodNotAllowed, 405, faults.CodeNotImplemented, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			provider := &apierrors.StatusError{ErrStatus: metav1.Status{
				Reason: test.reason,
				Code:   test.code,
				Details: &metav1.StatusDetails{
					Group:             "mindclade.dev",
					Kind:              "Run",
					Name:              "run-1",
					RetryAfterSeconds: 2,
				},
			}}
			qualified := Qualify(context.Background(), provider, "runs.Update", faults.Fields{"attempt": 2})
			if !faults.IsCode(qualified, test.want) || faults.IsRetryable(qualified) != test.retry {
				t.Fatalf("Qualify() = %v", qualified)
			}
			fields := faults.FieldsOf(qualified)
			if fields[FieldAPIReason] != string(test.reason) || fields[FieldHTTPStatus] != test.code || fields[FieldName] != "run-1" || fields["attempt"] != 2 {
				t.Fatalf("fields = %#v", fields)
			}
		})
	}
}

func TestQualifyContextAndGenericErrors(t *testing.T) {
	for _, test := range []struct {
		err  error
		code faults.Code
	}{
		{context.Canceled, faults.CodeCanceled},
		{context.DeadlineExceeded, faults.CodeDeadlineExceeded},
		{errors.New("unexpected provider error"), faults.CodeInternal},
	} {
		qualified := Qualify(context.Background(), test.err, "", nil)
		if !faults.IsCode(qualified, test.code) || !errors.Is(qualified, test.err) {
			t.Fatalf("Qualify(%v) = %v", test.err, qualified)
		}
	}
	if Qualify(context.Background(), nil, "test", nil) != nil {
		t.Fatal("Qualify(nil) changed nil")
	}
}

func TestQualifyObjectIncludesReference(t *testing.T) {
	provider := apierrors.NewNotFound(schema.GroupResource{Group: "mindclade.dev", Resource: "runs"}, "run-1")
	reference := ObjectReference{APIVersion: "mindclade.dev/v1", Kind: "Run", Namespace: "default", Name: "run-1", UID: "uid-1"}
	qualified := QualifyObject(context.Background(), provider, "runs.Get", reference, faults.Fields{"region": "us-central1"})
	fields := faults.FieldsOf(qualified)
	if fields[FieldNamespace] != "default" || fields[FieldUID] != "uid-1" || fields["region"] != "us-central1" {
		t.Fatalf("fields = %#v", fields)
	}
}
