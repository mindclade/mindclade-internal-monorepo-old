// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

package reflection

import (
	"net/http"
	"testing"
)

func TestHandlers(t *testing.T) {
	mounts, err := Handlers([]string{"mindclade.test.v1.TestService"}, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(mounts) != 2 {
		t.Fatalf("mounts=%d", len(mounts))
	}
	if err := Register(http.NewServeMux(), mounts...); err != nil {
		t.Fatal(err)
	}
	if _, err := Handlers(nil, false); err == nil {
		t.Fatal("expected missing service error")
	}
}

type pointerHandler struct{}

func (*pointerHandler) ServeHTTP(http.ResponseWriter, *http.Request) {}

func TestRegisterPrevalidatesAllMounts(t *testing.T) {
	mux := http.NewServeMux()
	mounts := []Mount{
		{Pattern: "/one", Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })},
		{Pattern: "/one", Handler: http.NotFoundHandler()},
	}
	if err := Register(mux, mounts...); err == nil {
		t.Fatal("expected duplicate mount error")
	}
	request, _ := http.NewRequest(http.MethodGet, "http://example.test/one", nil)
	recorder := &responseRecorder{header: http.Header{}}
	mux.ServeHTTP(recorder, request)
	if recorder.status != http.StatusNotFound {
		t.Fatalf("status=%d", recorder.status)
	}
}

func TestRegisterRejectsTypedNilHandler(t *testing.T) {
	var handler *pointerHandler
	if err := Register(http.NewServeMux(), Mount{Pattern: "/reflection", Handler: handler}); err == nil {
		t.Fatal("expected typed-nil handler error")
	}
}

type responseRecorder struct {
	header http.Header
	status int
}

func (recorder *responseRecorder) Header() http.Header    { return recorder.header }
func (recorder *responseRecorder) WriteHeader(status int) { recorder.status = status }
func (recorder *responseRecorder) Write(value []byte) (int, error) {
	if recorder.status == 0 {
		recorder.status = http.StatusOK
	}
	return len(value), nil
}
