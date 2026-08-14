// Copyright 2026 Mindclade. All rights reserved.
// Confidential and proprietary.

package observability

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"mindclade.internal/libs/go/auth"
	"mindclade.internal/libs/go/faults"
	"mindclade.internal/libs/go/requestmeta"
)

func TestLoggerEnrichmentAndRecursiveRedaction(t *testing.T) {
	var output bytes.Buffer
	resource, err := NewResource("control-plane", WithServiceVersion("1.2.3"))
	if err != nil {
		t.Fatal(err)
	}
	trace := TraceContext{TraceID: "4bf92f3577b34da6a3ce929d0e0e4736", SpanID: "00f067aa0ba902b7", Sampled: true}
	logger, err := NewLogger(
		slog.NewJSONHandler(&output, nil),
		resource,
		MustAttributes(faults.Fields{"deployment.ring": "staging", "service.name": "overridden"}),
		TraceContextProviderFunc(func(context.Context) (TraceContext, bool) { return trace, true }),
	)
	if err != nil {
		t.Fatal(err)
	}
	requestID, err := requestmeta.NewRequestIDAt(time.UnixMilli(1_700_000_000_000))
	if err != nil {
		t.Fatal(err)
	}
	ctx, err := requestmeta.WithMetadata(context.Background(), requestmeta.Metadata{RequestID: requestID})
	if err != nil {
		t.Fatal(err)
	}
	principal, err := auth.NewPrincipal(auth.PrincipalKindService, "scheduler", auth.WithIssuer("mindclade"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, err = auth.WithPrincipal(ctx, principal)
	if err != nil {
		t.Fatal(err)
	}
	private := faults.Wrap(errors.New("database password=secret"), faults.CodeUnavailable, "repository unavailable")
	logger.InfoContext(ctx, "request handled",
		slog.String("password", "secret"),
		slog.Group("credentials", slog.String("access_token", "token"), slog.String("mode", "service")),
		slog.Any("failure", private),
	)

	var record map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(output.Bytes()), &record); err != nil {
		t.Fatalf("decode log: %v\n%s", err, output.String())
	}
	assertLogField(t, record, "service.name", "control-plane")
	assertLogField(t, record, "service.version", "1.2.3")
	assertLogField(t, record, "deployment.ring", "staging")
	assertLogField(t, record, "request.id", requestID.String())
	assertLogField(t, record, "trace.id", trace.TraceID)
	assertLogField(t, record, "password", faults.RedactedValue)
	credentials, ok := record["credentials"].(map[string]any)
	if !ok {
		t.Fatalf("credentials = %#v", record["credentials"])
	}
	assertLogField(t, credentials, "access_token", faults.RedactedValue)
	assertLogField(t, credentials, "mode", "service")
	assertLogField(t, record, "failure", "repository unavailable")
	if strings.Contains(output.String(), "database") || strings.Contains(output.String(), "secret") || strings.Contains(output.String(), "scheduler") {
		t.Fatalf("private data leaked: %s", output.String())
	}
}

func TestLogErrorUsesPublicFaultInformation(t *testing.T) {
	var output bytes.Buffer
	resource, _ := NewResource("worker")
	logger, _ := NewLogger(slog.NewJSONHandler(&output, nil), resource, Attributes{}, nil)
	failure := faults.Wrap(errors.New("private SQL detail"), faults.CodeUnavailable, "dependency unavailable",
		faults.WithReason("dependency_unavailable"),
		faults.WithOperation("worker.Run"),
	)
	LogError(context.Background(), logger, slog.LevelError, "operation failed", failure)
	text := output.String()
	for _, expected := range []string{"dependency unavailable", "dependency_unavailable", "worker.Run"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("log missing %q: %s", expected, text)
		}
	}
	if strings.Contains(text, "private SQL") {
		t.Fatalf("private cause leaked: %s", text)
	}
}

func TestLoggerContextHelpers(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
	ctx, err := ContextWithLogger(context.Background(), logger)
	if err != nil {
		t.Fatal(err)
	}
	if LoggerFromContext(ctx, nil) != logger {
		t.Fatal("context logger not returned")
	}
	if _, err := ContextWithLogger(nil, logger); !errors.Is(err, ErrNilContext) {
		t.Fatalf("ContextWithLogger(nil) error = %v", err)
	}
	if _, err := ContextWithLogger(context.Background(), nil); !errors.Is(err, ErrNilHandler) {
		t.Fatalf("ContextWithLogger(nil logger) error = %v", err)
	}
}

func assertLogField(t *testing.T, fields map[string]any, key string, want any) {
	t.Helper()
	if got := fields[key]; got != want {
		t.Fatalf("%s = %#v, want %#v; fields=%#v", key, got, want, fields)
	}
}
