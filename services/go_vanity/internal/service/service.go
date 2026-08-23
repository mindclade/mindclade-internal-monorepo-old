// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

// Package service composes the public vanity endpoint, private metrics endpoint,
// probes, and graceful process lifecycle.
package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"sync/atomic"
	"time"

	"go.mindclade.dev/services/go_vanity/internal/vanity"
)

const shutdownTimeout = 15 * time.Second

var latencyBuckets = [...]float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1}

// Config is the complete runtime configuration. None of these values are
// credentials, and the service requires no cloud or Kubernetes identity.
type Config struct {
	ModulePath string
	RepoURL    string
	DocsURL    string
}

// Runtime owns both listeners. The application and metrics surfaces are
// intentionally separate so the public Service never exposes operational data.
type Runtime struct {
	appServer     *http.Server
	metricsServer *http.Server
	metrics       *metrics
}

// New validates config and builds an inert Runtime. Readiness changes only when
// Serve owns both already-bound listeners.
func New(config Config) (*Runtime, error) {
	handler, err := vanity.New(config.DocsURL, vanity.Rule{
		Prefix:  config.ModulePath,
		VCS:     "git",
		RepoURL: config.RepoURL,
	})
	if err != nil {
		return nil, err
	}

	runtimeMetrics := &metrics{}
	appMux := http.NewServeMux()
	appMux.HandleFunc("GET /healthz", func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusOK)
	})
	appMux.HandleFunc("GET /readyz", func(response http.ResponseWriter, _ *http.Request) {
		if !runtimeMetrics.ready.Load() {
			http.Error(response, "not ready", http.StatusServiceUnavailable)
			return
		}
		response.WriteHeader(http.StatusOK)
	})
	appMux.Handle("/", handler)

	metricsMux := http.NewServeMux()
	metricsMux.Handle("GET /metrics", runtimeMetrics)

	return &Runtime{
		appServer:     newHTTPServer(runtimeMetrics.instrument(appMux)),
		metricsServer: newHTTPServer(metricsMux),
		metrics:       runtimeMetrics,
	}, nil
}

func newHTTPServer(handler http.Handler) *http.Server {
	return &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}
}

// Serve runs the application and metrics servers as one lifecycle. An
// unexpected failure of either listener drains the other. Cancellation marks
// readiness false before allowing up to 15 seconds for in-flight requests.
func (runtime *Runtime) Serve(ctx context.Context, appListener, metricsListener net.Listener) error {
	if ctx == nil {
		return errors.New("service: context is required")
	}
	if appListener == nil || metricsListener == nil {
		return errors.New("service: both listeners are required")
	}

	serveErrors := make(chan error, 2)
	serve := func(name string, server *http.Server, listener net.Listener) {
		err := server.Serve(listener)
		if errors.Is(err, http.ErrServerClosed) || errors.Is(err, net.ErrClosed) {
			err = nil
		}
		if err != nil {
			err = fmt.Errorf("%s listener: %w", name, err)
		}
		serveErrors <- err
	}

	runtime.metrics.ready.Store(true)
	go serve("application", runtime.appServer, appListener)
	go serve("metrics", runtime.metricsServer, metricsListener)

	var serveErr error
	select {
	case <-ctx.Done():
	case serveErr = <-serveErrors:
	}

	runtime.metrics.ready.Store(false)
	// Runtime owns both listeners. Close them explicitly before draining so
	// cancellation cannot race ahead of Server.Serve registering a listener and
	// leave a late-starting accept loop blocked forever.
	listenerCloseErr := errors.Join(
		ignoreClosed(appListener.Close()),
		ignoreClosed(metricsListener.Close()),
	)
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	shutdownErrors := make(chan error, 2)
	go func() { shutdownErrors <- runtime.appServer.Shutdown(shutdownCtx) }()
	go func() { shutdownErrors <- runtime.metricsServer.Shutdown(shutdownCtx) }()
	shutdownErr := errors.Join(<-shutdownErrors, <-shutdownErrors)

	// Collect both Serve results. If one result triggered shutdown, only the
	// other remains; after cancellation both remain. Buffered sends prevent a
	// server exit from depending on this goroutine's scheduling.
	remaining := 2
	if serveErr != nil {
		remaining = 1
	} else if ctx.Err() == nil {
		// A listener may close cleanly without context cancellation. That is still
		// an unexpected lifecycle event, even though net/http reports no error.
		remaining = 1
		serveErr = errors.New("listener stopped unexpectedly")
	}
	for range remaining {
		serveErr = errors.Join(serveErr, <-serveErrors)
	}

	return errors.Join(serveErr, listenerCloseErr, shutdownErr)
}

func ignoreClosed(err error) error {
	if errors.Is(err, net.ErrClosed) {
		return nil
	}
	return err
}

type metrics struct {
	ready          atomic.Bool
	requests       [4]atomic.Uint64
	latencyBuckets [len(latencyBuckets) + 1]atomic.Uint64
	latencyCount   atomic.Uint64
	latencyNanos   atomic.Uint64
}

func (runtimeMetrics *metrics) instrument(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		start := time.Now()
		tracked := &statusWriter{ResponseWriter: response, status: http.StatusOK}
		next.ServeHTTP(tracked, request)
		runtimeMetrics.observe(tracked.status, time.Since(start))
	})
}

func (runtimeMetrics *metrics) observe(status int, duration time.Duration) {
	class := status/100 - 2
	if class < 0 || class >= len(runtimeMetrics.requests) {
		class = len(runtimeMetrics.requests) - 1
	}
	runtimeMetrics.requests[class].Add(1)
	runtimeMetrics.latencyCount.Add(1)
	runtimeMetrics.latencyNanos.Add(uint64(duration))
	seconds := duration.Seconds()
	for index, upper := range latencyBuckets {
		if seconds <= upper {
			runtimeMetrics.latencyBuckets[index].Add(1)
		}
	}
	runtimeMetrics.latencyBuckets[len(latencyBuckets)].Add(1)
}

func (runtimeMetrics *metrics) ServeHTTP(response http.ResponseWriter, _ *http.Request) {
	response.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	response.Header().Set("Cache-Control", "no-store")
	_, _ = io.WriteString(response, "# HELP mindclade_go_vanity_build_info Constant build identity for the go-vanity process.\n")
	_, _ = io.WriteString(response, "# TYPE mindclade_go_vanity_build_info gauge\n")
	_, _ = io.WriteString(response, "mindclade_go_vanity_build_info 1\n")
	_, _ = io.WriteString(response, "# HELP mindclade_go_vanity_ready Whether both HTTP listeners are accepting traffic.\n")
	_, _ = io.WriteString(response, "# TYPE mindclade_go_vanity_ready gauge\n")
	ready := "0"
	if runtimeMetrics.ready.Load() {
		ready = "1"
	}
	_, _ = fmt.Fprintf(response, "mindclade_go_vanity_ready %s\n", ready)
	_, _ = io.WriteString(response, "# HELP mindclade_go_vanity_http_requests_total Application HTTP requests by bounded status class.\n")
	_, _ = io.WriteString(response, "# TYPE mindclade_go_vanity_http_requests_total counter\n")
	for index, class := range [...]string{"2xx", "3xx", "4xx", "5xx"} {
		_, _ = fmt.Fprintf(response, "mindclade_go_vanity_http_requests_total{code_class=%q} %d\n", class, runtimeMetrics.requests[index].Load())
	}
	_, _ = io.WriteString(response, "# HELP mindclade_go_vanity_http_request_duration_seconds Application HTTP request duration.\n")
	_, _ = io.WriteString(response, "# TYPE mindclade_go_vanity_http_request_duration_seconds histogram\n")
	for index, upper := range latencyBuckets {
		_, _ = fmt.Fprintf(response, "mindclade_go_vanity_http_request_duration_seconds_bucket{le=%q} %d\n", strconv.FormatFloat(upper, 'f', -1, 64), runtimeMetrics.latencyBuckets[index].Load())
	}
	_, _ = fmt.Fprintf(response, "mindclade_go_vanity_http_request_duration_seconds_bucket{le=\"+Inf\"} %d\n", runtimeMetrics.latencyBuckets[len(latencyBuckets)].Load())
	_, _ = fmt.Fprintf(response, "mindclade_go_vanity_http_request_duration_seconds_sum %s\n", strconv.FormatFloat(float64(runtimeMetrics.latencyNanos.Load())/float64(time.Second), 'f', 9, 64))
	_, _ = fmt.Fprintf(response, "mindclade_go_vanity_http_request_duration_seconds_count %d\n", runtimeMetrics.latencyCount.Load())
}

type statusWriter struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (writer *statusWriter) WriteHeader(status int) {
	if writer.wroteHeader {
		return
	}
	writer.wroteHeader = true
	writer.status = status
	writer.ResponseWriter.WriteHeader(status)
}

// Unwrap preserves optional ResponseController capabilities of the underlying
// writer while still allowing status accounting.
func (writer *statusWriter) Unwrap() http.ResponseWriter { return writer.ResponseWriter }
