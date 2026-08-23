// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

package gateway

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.mindclade.dev/libs/go/storage/blob"
	"go.mindclade.dev/libs/go/storage/blob/memory"
)

func newGateway(t *testing.T, mode Mode, maximum int64) (*Gateway, *memory.Store) {
	t.Helper()
	store, err := memory.New(memory.WithMaximumObjectBytes(maximum))
	if err != nil {
		t.Fatalf("memory.New() error = %v", err)
	}
	gateway, err := New(store, Config{
		Mode:             mode,
		MaximumBodyBytes: maximum,
		TemporaryDir:     t.TempDir(),
	}, nil)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return gateway, store
}

func digest(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}

func request(t *testing.T, handler http.Handler, method, path string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, path, bytes.NewReader(body))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func TestCASRoundTripAndHead(t *testing.T) {
	gateway, _ := newGateway(t, ModeWrite, 1024)
	payload := []byte("content-addressed")
	path := "/cas/" + digest(payload)
	if response := request(t, gateway, http.MethodPut, path, payload); response.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, body = %q", response.Code, response.Body.String())
	}
	head := request(t, gateway, http.MethodHead, path, nil)
	if head.Code != http.StatusOK || head.Header().Get("Content-Length") != "17" {
		t.Fatalf("HEAD status = %d, length = %q", head.Code, head.Header().Get("Content-Length"))
	}
	get := request(t, gateway, http.MethodGet, path, nil)
	if get.Code != http.StatusOK || !bytes.Equal(get.Body.Bytes(), payload) {
		t.Fatalf("GET status = %d, body = %q", get.Code, get.Body.String())
	}
}

func TestReadModeRejectsPutWithoutMutation(t *testing.T) {
	gateway, store := newGateway(t, ModeRead, 1024)
	payload := []byte("not-written")
	path := "/cas/" + digest(payload)
	response := request(t, gateway, http.MethodPut, path, payload)
	if response.Code != http.StatusMethodNotAllowed || response.Header().Get("X-Mindclade-Error-Code") != "read_only" {
		t.Fatalf("PUT status = %d, code = %q", response.Code, response.Header().Get("X-Mindclade-Error-Code"))
	}
	if response.Header().Get("Allow") != "GET, HEAD" {
		t.Fatalf("PUT Allow = %q", response.Header().Get("Allow"))
	}
	if _, err := store.Stat(context.Background(), blob.MustParseKey("cas/"+digest(payload))); err == nil {
		t.Fatal("read-only PUT created an object")
	}
}

func TestCASDigestMismatchIsRejected(t *testing.T) {
	gateway, _ := newGateway(t, ModeWrite, 1024)
	response := request(t, gateway, http.MethodPut, "/cas/"+strings.Repeat("0", 64), []byte("different"))
	if response.Code != http.StatusBadRequest || response.Header().Get("X-Mindclade-Error-Code") != "cas_digest_mismatch" {
		t.Fatalf("PUT status = %d, code = %q", response.Code, response.Header().Get("X-Mindclade-Error-Code"))
	}
}

func TestDuplicateWritesAreIdempotentButCollisionsFail(t *testing.T) {
	gateway, store := newGateway(t, ModeWrite, 1024)
	key := strings.Repeat("a", 64)
	path := "/ac/" + key
	payload := []byte("action-result")
	for attempt := 0; attempt < 2; attempt++ {
		if response := request(t, gateway, http.MethodPut, path, payload); response.Code != http.StatusOK {
			t.Fatalf("PUT attempt %d status = %d", attempt, response.Code)
		}
	}
	existing, err := store.Stat(context.Background(), blob.MustParseKey("ac/"+key))
	if err != nil || existing.Generation != 1 {
		t.Fatalf("stored generation = %d, error = %v", existing.Generation, err)
	}
	collision := request(t, gateway, http.MethodPut, path, []byte("different-result"))
	if collision.Code != http.StatusConflict || collision.Header().Get("X-Mindclade-Error-Code") != "immutable_collision" {
		t.Fatalf("collision status = %d, code = %q", collision.Code, collision.Header().Get("X-Mindclade-Error-Code"))
	}
}

func TestMaximumBodyAndCanonicalPathAreEnforced(t *testing.T) {
	gateway, _ := newGateway(t, ModeWrite, 4)
	tooLarge := request(t, gateway, http.MethodPut, "/ac/"+strings.Repeat("b", 64), []byte("12345"))
	if tooLarge.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized PUT status = %d", tooLarge.Code)
	}
	for _, path := range []string{
		"/raw/" + strings.Repeat("c", 64),
		"/cas/" + strings.Repeat("C", 64),
		"/cas/not-a-digest",
		"/instance/cas/" + strings.Repeat("d", 64),
	} {
		if response := request(t, gateway, http.MethodGet, path, nil); response.Code != http.StatusBadRequest {
			t.Fatalf("GET %q status = %d", path, response.Code)
		}
	}
}

func TestConfiguredInstanceIsExact(t *testing.T) {
	store, err := memory.New()
	if err != nil {
		t.Fatal(err)
	}
	gateway, err := New(store, Config{Mode: ModeRead, InstanceName: "mindclade/v1", MaximumBodyBytes: 1024, TemporaryDir: t.TempDir()}, nil)
	if err != nil {
		t.Fatal(err)
	}
	valid := request(t, gateway, http.MethodGet, "/mindclade/v1/cas/"+strings.Repeat("e", 64), nil)
	if valid.Code != http.StatusNotFound {
		t.Fatalf("configured instance status = %d", valid.Code)
	}
	invalid := request(t, gateway, http.MethodGet, "/cas/"+strings.Repeat("e", 64), nil)
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("missing instance status = %d", invalid.Code)
	}
}

func TestMetricsAreRedactedAndCounted(t *testing.T) {
	gateway, _ := newGateway(t, ModeRead, 1024)
	path := "/cas/" + strings.Repeat("f", 64)
	_ = request(t, gateway, http.MethodGet, path, nil)
	metrics := request(t, gateway, http.MethodGet, "/metrics", nil)
	if metrics.Code != http.StatusOK || !strings.Contains(metrics.Body.String(), `"get_miss":1`) {
		t.Fatalf("metrics status = %d, body = %q", metrics.Code, metrics.Body.String())
	}
	if strings.Contains(metrics.Body.String(), strings.Repeat("f", 64)) {
		t.Fatal("metrics expose a cache digest")
	}
}

func TestGetRejectsRange(t *testing.T) {
	gateway, _ := newGateway(t, ModeRead, 1024)
	request := httptest.NewRequest(http.MethodGet, "/cas/"+strings.Repeat("1", 64), nil)
	request.Header.Set("Range", "bytes=0-1")
	response := httptest.NewRecorder()
	gateway.ServeHTTP(response, request)
	if response.Code != http.StatusRequestedRangeNotSatisfiable {
		t.Fatalf("range status = %d", response.Code)
	}
}

func TestResponseDoesNotExposeBackendError(t *testing.T) {
	gateway, _ := newGateway(t, ModeRead, 1024)
	response := request(t, gateway, http.MethodGet, "/cas/"+strings.Repeat("2", 64), nil)
	body, err := io.ReadAll(response.Result().Body)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), strings.Repeat("2", 64)) {
		t.Fatal("response exposes a cache digest")
	}
}
