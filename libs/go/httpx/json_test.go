// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package httpx

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"mindclade.internal/libs/go/faults"
)

func TestDecodeJSONStrict(t *testing.T) {
	request := httptest.NewRequest("POST", "/", strings.NewReader(`{"name":"x","unknown":true}`))
	request.Header.Set("Content-Type", "application/json")
	var value struct {
		Name string `json:"name"`
	}
	err := DecodeJSON(request, &value, 1024)
	if faults.CodeOf(err) != faults.CodeInvalidArgument {
		t.Fatalf("code = %s", faults.CodeOf(err))
	}
}

func TestDecodeJSONEnforcesExactBodyLimit(t *testing.T) {
	body := `{"name":"x"}`
	request := httptest.NewRequest("POST", "/", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	var exact struct {
		Name string `json:"name"`
	}
	if err := DecodeJSON(request, &exact, int64(len(body))); err != nil {
		t.Fatalf("exact limit: %v", err)
	}

	request = httptest.NewRequest("POST", "/", strings.NewReader(body+" "))
	request.Header.Set("Content-Type", "application/json")
	var oversized struct {
		Name string `json:"name"`
	}
	err := DecodeJSON(request, &oversized, int64(len(body)))
	if faults.CodeOf(err) != faults.CodeResourceExhausted {
		t.Fatalf("code = %s, err=%v", faults.CodeOf(err), err)
	}
	if faults.ReasonOf(err) != "request_body_too_large" {
		t.Fatalf("reason = %q", faults.ReasonOf(err))
	}
}

type unsupportedJSONValue struct {
	Channel chan int `json:"channel"`
}

type commitTrackingWriter struct {
	header    http.Header
	committed bool
	status    int
}

func (writer *commitTrackingWriter) Header() http.Header {
	if writer.header == nil {
		writer.header = make(http.Header)
	}
	return writer.header
}
func (writer *commitTrackingWriter) WriteHeader(status int) {
	writer.committed = true
	writer.status = status
}
func (writer *commitTrackingWriter) Write(payload []byte) (int, error) {
	writer.committed = true
	if writer.status == 0 {
		writer.status = http.StatusOK
	}
	return len(payload), nil
}

func TestWriteJSONEncodesBeforeCommit(t *testing.T) {
	writer := &commitTrackingWriter{}
	err := WriteJSON(writer, http.StatusOK, unsupportedJSONValue{Channel: make(chan int)})
	if faults.CodeOf(err) != faults.CodeInternal {
		t.Fatalf("code = %s, err=%v", faults.CodeOf(err), err)
	}
	if writer.committed {
		t.Fatal("response was committed before encoding succeeded")
	}
}

func TestWriteJSONSuppressesBodiesForBodylessStatuses(t *testing.T) {
	for _, status := range []int{http.StatusContinue, http.StatusNoContent, http.StatusNotModified} {
		recorder := httptest.NewRecorder()
		if err := WriteJSON(recorder, status, map[string]string{"value": "unexpected"}); err != nil {
			t.Fatalf("status %d: %v", status, err)
		}
		if recorder.Body.Len() != 0 {
			t.Fatalf("status %d body=%q", status, recorder.Body.String())
		}
		if recorder.Header().Get("Content-Type") != "" {
			t.Fatalf("status %d content-type=%q", status, recorder.Header().Get("Content-Type"))
		}
	}
}
