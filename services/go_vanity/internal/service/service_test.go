// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

package service

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const canonicalRepo = "https://github.com/mindclade/mindclade-internal-monorepo"

func newTestRuntime(t *testing.T) *Runtime {
	t.Helper()
	runtime, err := New(Config{
		ModulePath: "go.mindclade.dev",
		RepoURL:    canonicalRepo,
		DocsURL:    "https://docs.mindclade.dev",
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return runtime
}

func TestApplicationAndMetricsSurfacesAreSeparate(t *testing.T) {
	runtime := newTestRuntime(t)
	runtime.metrics.ready.Store(true)

	request := httptest.NewRequest(http.MethodGet, "http://go.mindclade.dev/?go-get=1", nil)
	response := httptest.NewRecorder()
	runtime.appServer.Handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), canonicalRepo) {
		t.Fatalf("vanity response = %d %q", response.Code, response.Body.String())
	}

	metricsOnApp := httptest.NewRecorder()
	runtime.appServer.Handler.ServeHTTP(
		metricsOnApp,
		httptest.NewRequest(http.MethodGet, "/metrics", nil),
	)
	if strings.Contains(metricsOnApp.Body.String(), "mindclade_go_vanity_http_requests_total") {
		t.Fatal("application listener exposed metrics")
	}

	metricsResponse := httptest.NewRecorder()
	runtime.metricsServer.Handler.ServeHTTP(
		metricsResponse,
		httptest.NewRequest(http.MethodGet, "/metrics", nil),
	)
	body := metricsResponse.Body.String()
	for _, want := range []string{
		"mindclade_go_vanity_ready 1",
		`mindclade_go_vanity_http_requests_total{code_class="2xx"} 1`,
		`mindclade_go_vanity_http_requests_total{code_class="3xx"} 1`,
		`mindclade_go_vanity_http_request_duration_seconds_bucket{le="+Inf"} 2`,
		"mindclade_go_vanity_http_request_duration_seconds_count 2",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("metrics missing %q\n%s", want, body)
		}
	}
	for _, forbidden := range []string{"go_get", "request_path", "module_path", canonicalRepo} {
		if strings.Contains(body, forbidden) {
			t.Errorf("metrics contain unbounded or sensitive value %q", forbidden)
		}
	}
	if got := strings.Count(body, "mindclade_go_vanity_http_requests_total{"); got != 4 {
		t.Errorf("request-series cardinality = %d, want 4", got)
	}
	if got := strings.Count(body, "mindclade_go_vanity_http_request_duration_seconds_bucket{"); got != 9 {
		t.Errorf("latency-bucket cardinality = %d, want 9", got)
	}

	metricsHealth := httptest.NewRecorder()
	runtime.metricsServer.Handler.ServeHTTP(
		metricsHealth,
		httptest.NewRequest(http.MethodGet, "/healthz", nil),
	)
	if metricsHealth.Code != http.StatusNotFound {
		t.Errorf("metrics /healthz status = %d, want 404", metricsHealth.Code)
	}
}

func TestServeCancelsAndDrainsBothListeners(t *testing.T) {
	runtime := newTestRuntime(t)
	appListener := listenLocal(t)
	metricsListener := listenLocal(t)

	ctx, cancel := context.WithCancel(context.Background())
	serveDone := make(chan error, 1)
	go func() { serveDone <- runtime.Serve(ctx, appListener, metricsListener) }()

	client := &http.Client{Timeout: time.Second}
	waitForStatus(t, client, "http://"+appListener.Addr().String()+"/readyz", http.StatusOK)
	waitForStatus(t, client, "http://"+metricsListener.Addr().String()+"/metrics", http.StatusOK)

	cancel()
	select {
	case err := <-serveDone:
		if err != nil {
			t.Fatalf("Serve after cancellation: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Serve did not stop both listeners after cancellation")
	}
	if runtime.metrics.ready.Load() {
		t.Fatal("runtime stayed ready after shutdown")
	}
}

func TestServeHandlesCancellationBeforeServersStart(t *testing.T) {
	runtime := newTestRuntime(t)
	appListener := listenLocal(t)
	metricsListener := listenLocal(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	serveDone := make(chan error, 1)
	go func() { serveDone <- runtime.Serve(ctx, appListener, metricsListener) }()
	select {
	case err := <-serveDone:
		if err != nil {
			t.Fatalf("Serve after prior cancellation: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Serve blocked when cancellation won the startup race")
	}
}

func TestServeFailsClosedWhenEitherListenerFails(t *testing.T) {
	runtime := newTestRuntime(t)
	listenerErr := errors.New("accept failed")
	metricsListener := listenLocal(t)

	err := runtime.Serve(
		context.Background(),
		&errorListener{err: listenerErr},
		metricsListener,
	)
	if !errors.Is(err, listenerErr) {
		t.Fatalf("Serve error = %v, want wrapped listener error", err)
	}
	if runtime.metrics.ready.Load() {
		t.Fatal("runtime stayed ready after listener failure")
	}
}

func TestServeRejectsIncompleteOwnership(t *testing.T) {
	runtime := newTestRuntime(t)
	if err := runtime.Serve(nil, nil, nil); err == nil || !strings.Contains(err.Error(), "context") {
		t.Fatalf("nil context error = %v", err)
	}
	if err := runtime.Serve(context.Background(), nil, nil); err == nil || !strings.Contains(err.Error(), "listeners") {
		t.Fatalf("nil listener error = %v", err)
	}
}

func listenLocal(t *testing.T) net.Listener {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	return listener
}

func waitForStatus(t *testing.T, client *http.Client, url string, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		response, err := client.Get(url)
		if err == nil {
			_, _ = io.Copy(io.Discard, response.Body)
			_ = response.Body.Close()
			if response.StatusCode == want {
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("%s did not return %d", url, want)
}

type errorListener struct {
	err error
}

func (listener *errorListener) Accept() (net.Conn, error) { return nil, listener.err }
func (listener *errorListener) Close() error              { return nil }
func (listener *errorListener) Addr() net.Addr            { return testAddress("error") }

type testAddress string

func (address testAddress) Network() string { return "test" }
func (address testAddress) String() string  { return string(address) }
