// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package transport

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"go.mindclade.dev/libs/go/identifiers"
	"go.mindclade.dev/libs/go/resourceversion"
)

func TestPreconditionsDecodeConditionalRequests(t *testing.T) {
	version, err := resourceversion.New(7, identifiers.MustParseDigest(
		"sha256:0000000000000000000000000000000000000000000000000000000000000001"))
	if err != nil {
		t.Fatal(err)
	}
	for name, testCase := range map[string]struct {
		header  string
		value   string
		want    resourceversion.Precondition
		present bool
		status  int
	}{
		"absent":        {status: http.StatusOK},
		"if-match etag": {header: "If-Match", value: version.ETag(), want: resourceversion.MatchVersion(version), present: true, status: http.StatusOK},
		"if-match any":  {header: "If-Match", value: "*", want: resourceversion.RequireExistence(), present: true, status: http.StatusOK},
		"if-none-match": {header: "If-None-Match", value: "*", want: resourceversion.RequireAbsence(), present: true, status: http.StatusOK},
		"malformed":     {header: "If-Match", value: "\"not-an-etag\"", status: http.StatusBadRequest},
	} {
		t.Run(name, func(t *testing.T) {
			var observed resourceversion.Precondition
			var present bool
			handler := Preconditions()(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
				observed, present = PreconditionFrom(request.Context())
			}))
			request := httptest.NewRequest(http.MethodPut, "/v1/models/m1", nil)
			if testCase.header != "" {
				request.Header.Set(testCase.header, testCase.value)
			}
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)
			if recorder.Code != testCase.status {
				t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
			}
			if present != testCase.present {
				t.Fatalf("present=%v", present)
			}
			if present && observed != testCase.want {
				t.Fatalf("precondition=%+v want=%+v", observed, testCase.want)
			}
		})
	}
}

func TestPreconditionsRejectConflictingHeaders(t *testing.T) {
	handler := Preconditions()(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("handler must not run")
	}))
	request := httptest.NewRequest(http.MethodPut, "/v1/models/m1", nil)
	request.Header.Set("If-Match", "*")
	request.Header.Set("If-None-Match", "*")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status=%d", recorder.Code)
	}
}
