// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package rpcfaults

import (
	"context"
	"errors"
	"testing"
	"time"

	"mindclade.internal/libs/go/faults"
	"mindclade.internal/libs/go/requestmeta"
)

func TestFromErrorSanitizesCauseAndFields(t *testing.T) {
	ctx := context.Background()
	requestID := requestmeta.MustParseRequestID("request_019c7af21b8276d2a0d522fe41739a21")
	var err error
	ctx, err = requestmeta.WithRequestID(ctx, requestID)
	if err != nil {
		t.Fatal(err)
	}

	source := faults.Wrap(
		errors.New("postgres password=secret"),
		faults.CodeUnavailable,
		"service temporarily unavailable",
		faults.WithReason("repository_unavailable"),
		faults.WithRetryPolicy(faults.DelayedRetry(3*time.Second, 4)),
		faults.WithFields(faults.Fields{
			faults.FieldResourceType: "training_run",
			faults.FieldResourceID:   "run_123",
			"api_key":                "super-secret",
			"arbitrary":              "must-not-cross-wire",
		}),
	)

	details := FromError(ctx, source)
	if details.Message != "service temporarily unavailable" {
		t.Fatalf("message = %q", details.Message)
	}
	if details.RequestID != requestID.String() {
		t.Fatalf("request ID = %q", details.RequestID)
	}
	if details.Resource.ID != "run_123" {
		t.Fatalf("resource ID = %q", details.Resource.ID)
	}
	if _, ok := details.Metadata["api_key"]; ok {
		t.Fatal("sensitive field crossed wire boundary")
	}
	if _, ok := details.Metadata["arbitrary"]; ok {
		t.Fatal("unapproved field crossed wire boundary")
	}
	if details.RetryAfter() != 3*time.Second {
		t.Fatalf("retry after = %v", details.RetryAfter())
	}
}

func TestToErrorRoundTripPreservesSafeClassification(t *testing.T) {
	input := Details{
		Code:      faults.CodeNotFound,
		Message:   "model was not found",
		Reason:    "model_not_found",
		Operation: "models.Registry.Get",
		RequestID: "request_019c7af21b8276d2a0d522fe41739a21",
		Resource:  Resource{Type: "model", ID: "model_123"},
		Retry:     faults.NoRetry(),
	}
	err := ToError(input)
	if faults.CodeOf(err) != faults.CodeNotFound {
		t.Fatalf("code = %s", faults.CodeOf(err))
	}
	if faults.PublicMessageOf(err) != input.Message {
		t.Fatalf("message = %q", faults.PublicMessageOf(err))
	}
	if faults.ReasonOf(err) != input.Reason {
		t.Fatalf("reason = %q", faults.ReasonOf(err))
	}
	fields := faults.FieldsOf(err)
	if fields[faults.FieldResourceID] != "model_123" {
		t.Fatalf("fields = %#v", fields)
	}
}
