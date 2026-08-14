// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

package grpcx

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"mindclade.internal/libs/go/faults"
)

func TestStatusRoundTripIsSafe(t *testing.T) {
	source := faults.Wrap(
		errors.New("postgres password=secret"),
		faults.CodeUnavailable,
		"service unavailable",
		faults.WithReason("backend_unavailable"),
		faults.WithOperation("registry.Resolve"),
		faults.WithRequestID("request_019c7af21b8276d2a0d522fe41739a21"),
		faults.WithTraceID("trace-123"),
		faults.WithFields(faults.Fields{
			faults.FieldResourceType: "model",
			faults.FieldResourceID:   "model_123",
			faults.FieldTenantID:     "tenant_123",
			"password":               "secret",
		}),
		faults.WithRetryPolicy(faults.DelayedRetry(time.Second, 3)),
	)
	wire := StatusFromError(context.Background(), source)
	if strings.Contains(wire.Error(), "password") || strings.Contains(wire.Error(), "secret") {
		t.Fatal("cause leaked")
	}
	decoded := ErrorFromStatus(wire)
	if faults.CodeOf(decoded) != faults.CodeUnavailable {
		t.Fatalf("code=%s", faults.CodeOf(decoded))
	}
	if faults.ReasonOf(decoded) != "backend_unavailable" {
		t.Fatalf("reason=%q", faults.ReasonOf(decoded))
	}
	if faults.OperationOf(decoded) != "registry.Resolve" {
		t.Fatalf("operation=%q", faults.OperationOf(decoded))
	}
	fields := faults.FieldsOf(decoded)
	if fields[faults.FieldRequestID] != "request_019c7af21b8276d2a0d522fe41739a21" {
		t.Fatalf("request_id=%v", fields[faults.FieldRequestID])
	}
	if fields[faults.FieldResourceType] != "model" || fields[faults.FieldResourceID] != "model_123" {
		t.Fatalf("resource fields=%v", fields)
	}
	if fields[faults.FieldTenantID] != "tenant_123" {
		t.Fatalf("tenant=%v", fields[faults.FieldTenantID])
	}
	if _, ok := fields["password"]; ok {
		t.Fatal("sensitive metadata survived transport")
	}
	policy := faults.RetryPolicyOf(decoded)
	if policy.Kind != faults.RetryKindAfter || policy.After != time.Second || policy.MaxAttempts != 3 {
		t.Fatalf("retry=%#v", policy)
	}
	if errors.Is(decoded, source) {
		t.Fatal("decoded fault retained server cause")
	}
}

func TestStatusRoundTripPreservesSpecificConflictCode(t *testing.T) {
	wire := StatusFromError(context.Background(), faults.New(faults.CodeConflict, "revision conflict"))
	if status.Code(wire) != codes.Aborted {
		t.Fatalf("wire code=%s", status.Code(wire))
	}
	decoded := ErrorFromStatus(wire)
	if faults.CodeOf(decoded) != faults.CodeConflict {
		t.Fatalf("decoded code=%s", faults.CodeOf(decoded))
	}
}

func TestErrorFromStatusRejectsIncompatibleFaultCodeDetail(t *testing.T) {
	value := status.New(codes.Internal, "internal server error")
	value, err := value.WithDetails(&errdetails.ErrorInfo{
		Domain: ErrorDomain,
		Reason: "forged",
		Metadata: map[string]string{
			"fault_code": faults.CodeNotFound.String(),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	decoded := ErrorFromStatus(value.Err())
	if faults.CodeOf(decoded) != faults.CodeInternal {
		t.Fatalf("decoded code=%s", faults.CodeOf(decoded))
	}
}

func TestStatusFromErrorDropsForeignDetails(t *testing.T) {
	value := status.New(codes.Internal, "public message")
	value, err := value.WithDetails(&errdetails.DebugInfo{Detail: "stack password=secret"})
	if err != nil {
		t.Fatal(err)
	}
	wire := StatusFromError(context.Background(), value.Err())
	converted, ok := status.FromError(wire)
	if !ok {
		t.Fatal("expected gRPC status")
	}
	for _, detail := range converted.Details() {
		if _, ok := detail.(*errdetails.DebugInfo); ok {
			t.Fatal("foreign debug detail crossed boundary")
		}
	}
	if strings.Contains(wire.Error(), "password") || strings.Contains(wire.Error(), "stack") {
		t.Fatal("foreign detail leaked in error text")
	}
}

func TestStatusConversionPreservesEOF(t *testing.T) {
	if !errors.Is(StatusFromError(context.Background(), io.EOF), io.EOF) {
		t.Fatal("server EOF not preserved")
	}
	if !errors.Is(ErrorFromStatus(io.EOF), io.EOF) {
		t.Fatal("client EOF not preserved")
	}
}

func TestErrorFromStatusClassifiesLocalTransportFailure(t *testing.T) {
	cause := errors.New("dial tcp: refused")
	decoded := ErrorFromStatus(cause)
	if !errors.Is(decoded, cause) {
		t.Fatal("local transport cause was not preserved")
	}
	if faults.CodeOf(decoded) != faults.CodeUnavailable {
		t.Fatalf("code=%s", faults.CodeOf(decoded))
	}
	if faults.ReasonOf(decoded) != "grpc_transport_failure" {
		t.Fatalf("reason=%q", faults.ReasonOf(decoded))
	}
}

func TestWrappedStatusDoesNotExposeWrapperDiagnostics(t *testing.T) {
	wrapped := fmt.Errorf("database password=secret: %w", status.Error(codes.Internal, "public message"))
	wire := StatusFromError(context.Background(), wrapped)
	converted, ok := status.FromError(wire)
	if !ok {
		t.Fatal("expected gRPC status")
	}
	if converted.Message() != "public message" {
		t.Fatalf("message = %q", converted.Message())
	}
	if strings.Contains(converted.Message(), "secret") {
		t.Fatal("wrapper diagnostics leaked")
	}

	local := ErrorFromStatus(wrapped)
	if faults.PublicMessageOf(local) != "public message" {
		t.Fatalf("local message = %q", faults.PublicMessageOf(local))
	}
}
