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
	"os"
	"strings"
	"testing"
	"time"

	"go.mindclade.dev/libs/go/storage/blob"
	"go.mindclade.dev/libs/go/storage/blob/memory"
)

type corruptingStore struct {
	Store
	replacement []byte
}

func (store corruptingStore) Open(ctx context.Context, key blob.Key, options blob.GetOptions) (blob.Object, error) {
	object, err := store.Store.Open(ctx, key, options)
	if err != nil {
		return blob.Object{}, err
	}
	if err := object.Close(); err != nil {
		return blob.Object{}, err
	}
	object.Body = io.NopCloser(bytes.NewReader(store.replacement))
	return object, nil
}

type blockingReader struct {
	started chan<- struct{}
	release <-chan struct{}
}

func (reader *blockingReader) Read([]byte) (int, error) {
	close(reader.started)
	<-reader.release
	return 0, io.EOF
}

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

func TestGetRejectsSameSizeCorruptObjectBeforeWritingSuccess(t *testing.T) {
	writer, store := newGateway(t, ModeWrite, 1024)
	payload := []byte("valid-action-result")
	path := "/ac/" + strings.Repeat("a", 64)
	if response := request(t, writer, http.MethodPut, path, payload); response.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, body = %q", response.Code, response.Body.String())
	}
	corrupt := append([]byte(nil), payload...)
	corrupt[0] ^= 0xff
	temporaryDirectory := t.TempDir()
	reader, err := New(corruptingStore{Store: store, replacement: corrupt}, Config{
		Mode:             ModeRead,
		MaximumBodyBytes: 1024,
		TemporaryDir:     temporaryDirectory,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	response := request(t, reader, http.MethodGet, path, nil)
	if response.Code != http.StatusBadGateway || response.Header().Get("X-Mindclade-Error-Code") != "backend_error" {
		t.Fatalf("GET status = %d, code = %q", response.Code, response.Header().Get("X-Mindclade-Error-Code"))
	}
	if response.Body.String() != "Bad Gateway\n" {
		t.Fatalf("GET body = %q; expected only the redacted backend error", response.Body.String())
	}
	entries, err := os.ReadDir(temporaryDirectory)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("GET left %d staged files", len(entries))
	}
}

func TestConcurrentGetAndPutStagingIsBoundAndContextAware(t *testing.T) {
	store, err := memory.New(memory.WithMaximumObjectBytes(1024))
	if err != nil {
		t.Fatal(err)
	}
	gateway, err := New(store, Config{
		Mode:                     ModeWrite,
		MaximumBodyBytes:         1024,
		MaximumConcurrentStaging: 1,
		TemporaryDir:             t.TempDir(),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	release := make(chan struct{})
	released := false
	defer func() {
		if !released {
			close(release)
		}
	}()
	putResponse := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		putRequest := httptest.NewRequest(http.MethodPut, "/ac/"+strings.Repeat("b", 64), &blockingReader{
			started: started,
			release: release,
		})
		response := httptest.NewRecorder()
		gateway.ServeHTTP(response, putRequest)
		putResponse <- response
	}()
	<-started

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	getRequest := httptest.NewRequest(http.MethodGet, "/ac/"+strings.Repeat("c", 64), nil).WithContext(ctx)
	getResponses := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		response := httptest.NewRecorder()
		gateway.ServeHTTP(response, getRequest)
		getResponses <- response
	}()
	deadline := time.Now().Add(5 * time.Second)
	for gateway.metrics.stagingWait.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if gateway.metrics.stagingWait.Load() != 1 {
		t.Fatal("GET did not block on the shared staging semaphore")
	}
	cancel()
	var getResponse *httptest.ResponseRecorder
	select {
	case getResponse = <-getResponses:
	case <-time.After(5 * time.Second):
		t.Fatal("queued GET did not observe context cancellation")
	}
	if getResponse.Code != http.StatusRequestTimeout || getResponse.Header().Get("X-Mindclade-Error-Code") != "staging_wait_canceled" {
		t.Fatalf("queued GET status = %d, code = %q", getResponse.Code, getResponse.Header().Get("X-Mindclade-Error-Code"))
	}
	close(release)
	released = true
	select {
	case response := <-putResponse:
		if response.Code != http.StatusOK {
			t.Fatalf("PUT status = %d, body = %q", response.Code, response.Body.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("PUT did not resume after staging slot was released")
	}
	metrics := request(t, gateway, http.MethodGet, "/metrics", nil).Body.String()
	for _, expected := range []string{
		`"maximum_concurrent_staging":1`,
		`"staging_active":0`,
		`"staging_peak":1`,
		`"staging_wait":1`,
		`"staging_wait_canceled":1`,
	} {
		if !strings.Contains(metrics, expected) {
			t.Fatalf("metrics = %q, missing %q", metrics, expected)
		}
	}
}

func TestNewRejectsUnboundedConcurrentStaging(t *testing.T) {
	store, err := memory.New()
	if err != nil {
		t.Fatal(err)
	}
	_, err = New(store, Config{
		Mode:                     ModeRead,
		MaximumBodyBytes:         1024,
		MaximumConcurrentStaging: MaximumConcurrentStaging + 1,
		TemporaryDir:             t.TempDir(),
	}, nil)
	if err == nil {
		t.Fatal("New accepted an unbounded staging configuration")
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
	if metrics.Code != http.StatusOK || !strings.Contains(metrics.Body.String(), `"get_miss":1`) || !strings.Contains(metrics.Body.String(), `"maximum_concurrent_staging":2`) || !strings.Contains(metrics.Body.String(), `"schema_version":2`) {
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
