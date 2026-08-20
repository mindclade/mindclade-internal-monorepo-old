// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package httpx

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go.mindclade.dev/libs/go/faults"
)

func TestWriteErrorNeverExposesCause(t *testing.T) {
	recorder := httptest.NewRecorder()
	source := faults.Wrap(errors.New("database password=secret"), faults.CodeUnavailable, "service temporarily unavailable",
		faults.WithReason("repository_unavailable"), faults.WithRetryPolicy(faults.DelayedRetry(1500*time.Millisecond, 3)))
	WriteError(context.Background(), recorder, source, "/runs?token=secret")
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d", recorder.Code)
	}
	body := recorder.Body.String()
	if strings.Contains(body, "password") || strings.Contains(body, "token=secret") {
		t.Fatalf("unsafe body: %s", body)
	}
	if recorder.Header().Get("Retry-After") != "2" {
		t.Fatalf("retry-after = %q", recorder.Header().Get("Retry-After"))
	}
}

func TestDecodeError(t *testing.T) {
	response := &http.Response{
		StatusCode: http.StatusNotFound,
		Header:     http.Header{"Content-Type": []string{ProblemMediaType}},
		Body:       ioNopCloser(`{"title":"Not Found","status":404,"detail":"model was not found","code":"not_found","reason":"model_not_found"}`),
	}
	err := DecodeError(response)
	if faults.CodeOf(err) != faults.CodeNotFound {
		t.Fatalf("code = %s", faults.CodeOf(err))
	}
	if faults.ReasonOf(err) != "model_not_found" {
		t.Fatalf("reason = %q", faults.ReasonOf(err))
	}
}

type stringReadCloser struct{ *strings.Reader }

func (stringReadCloser) Close() error           { return nil }
func ioNopCloser(value string) stringReadCloser { return stringReadCloser{strings.NewReader(value)} }

func TestProblemRejectsInconsistentStatusAndCode(t *testing.T) {
	problem := Problem{
		Title:  "Internal Server Error",
		Status: http.StatusInternalServerError,
		Detail: "failed",
		Code:   faults.CodeNotFound.String(),
	}
	if err := problem.Validate(); err == nil {
		t.Fatal("expected inconsistent problem to be rejected")
	}
	if code := faults.CodeOf(problem.Error()); code != faults.CodeInternal {
		t.Fatalf("code = %s", code)
	}
}

func TestDecodeErrorRejectsMismatchedWireStatus(t *testing.T) {
	response := &http.Response{
		StatusCode: http.StatusInternalServerError,
		Header:     http.Header{"Content-Type": []string{ProblemMediaType}},
		Body:       ioNopCloser(`{"title":"Not Found","status":404,"detail":"hidden downgrade","code":"not_found"}`),
	}
	err := DecodeError(response)
	if faults.CodeOf(err) != faults.CodeInternal {
		t.Fatalf("code = %s", faults.CodeOf(err))
	}
	if faults.PublicMessageOf(err) == "hidden downgrade" {
		t.Fatal("trusted inconsistent problem body")
	}
}

func TestDecodeErrorRejectsOversizedProblem(t *testing.T) {
	padding := strings.Repeat("x", MaximumProblemBytes)
	response := &http.Response{
		StatusCode: http.StatusBadRequest,
		Header:     http.Header{"Content-Type": []string{ProblemMediaType}},
		Body:       ioNopCloser(`{"title":"Bad Request","status":400,"detail":"` + padding + `","code":"invalid_argument"}`),
	}
	err := DecodeError(response)
	if faults.CodeOf(err) != faults.CodeInvalidArgument {
		t.Fatalf("code = %s", faults.CodeOf(err))
	}
	if strings.Contains(faults.PublicMessageOf(err), padding[:64]) {
		t.Fatal("trusted oversized problem body")
	}
}
