// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package connectx

import (
	"context"
	"errors"
	"testing"
	"time"

	"connectrpc.com/connect"

	"go.mindclade.dev/libs/go/faults"
)

func TestEncodeDecodeError(t *testing.T) {
	original := faults.Wrap(
		errors.New("database password=secret"),
		faults.CodeUnavailable,
		"registry unavailable",
		faults.WithReason("registry_unavailable"),
		faults.WithOperation("models.Registry.Resolve"),
		faults.WithRequestID("request_019c7af21b8276d2a0d522fe41739a21"),
		faults.WithRetryPolicy(faults.DelayedRetry(1500*time.Millisecond, 4)),
	)
	encoded := EncodeError(context.Background(), original)
	var connectErr *connect.Error
	if !errors.As(encoded, &connectErr) {
		t.Fatalf("expected connect error: %T", encoded)
	}
	if connectErr.Code() != connect.CodeUnavailable {
		t.Fatalf("code = %v", connectErr.Code())
	}
	if connectErr.Message() != "registry unavailable" {
		t.Fatalf("message = %q", connectErr.Message())
	}
	if connectErr.Meta().Get(HeaderRetryAfterMillis) != "1500" {
		t.Fatalf("retry metadata = %q", connectErr.Meta().Get(HeaderRetryAfterMillis))
	}
	if contains := errors.Is(encoded, original); contains {
		t.Fatal("wire error retained server cause")
	}
	decoded := DecodeError(encoded)
	if faults.CodeOf(decoded) != faults.CodeUnavailable {
		t.Fatalf("decoded code = %s", faults.CodeOf(decoded))
	}
	if faults.ReasonOf(decoded) != "registry_unavailable" {
		t.Fatalf("reason = %q", faults.ReasonOf(decoded))
	}
	if faults.PublicMessageOf(decoded) != "registry unavailable" {
		t.Fatalf("message = %q", faults.PublicMessageOf(decoded))
	}
	policy := faults.RetryPolicyOf(decoded)
	if policy.After != 1500*time.Millisecond || policy.MaxAttempts != 4 {
		t.Fatalf("policy = %#v", policy)
	}
	if errors.Is(decoded, original) {
		t.Fatal("decoded error retained server cause")
	}
}

func TestDecodeLocalTransportError(t *testing.T) {
	cause := context.DeadlineExceeded
	err := DecodeError(cause)
	if !errors.Is(err, cause) {
		t.Fatal("local transport cause was not preserved")
	}
	if faults.CodeOf(err) != faults.CodeDeadlineExceeded {
		t.Fatalf("code = %s", faults.CodeOf(err))
	}
}

func TestDecodeGenericLocalTransportError(t *testing.T) {
	cause := errors.New("dial tcp: connection refused")
	err := DecodeError(cause)
	if !errors.Is(err, cause) {
		t.Fatal("local cause was not preserved")
	}
	if faults.CodeOf(err) != faults.CodeUnavailable {
		t.Fatalf("code = %s", faults.CodeOf(err))
	}
	if faults.ReasonOf(err) != "connect_transport_failure" {
		t.Fatalf("reason = %q", faults.ReasonOf(err))
	}
	if !faults.RetryPolicyOf(err).Retryable() {
		t.Fatal("transport failure should carry retry intent")
	}
}
