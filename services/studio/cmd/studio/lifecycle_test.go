// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package main

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"testing"
	"time"

	"go.mindclade.dev/libs/go/clock"
	libhttpx "go.mindclade.dev/libs/go/httpx"
	"go.mindclade.dev/services/studio/internal/server"
)

// The whole point of the drain: readiness must FAIL, and the propagation window
// must elapse, while the listener is STILL SERVING.
//
// Closing the listener at signal time strands whatever the load balancer is
// still routing here as 502s, and that is invisible in any test that only
// checks the process exits cleanly. This drives a fake clock so the ordering is
// asserted rather than timed.
func TestDrainFailsReadinessWhileStillServing(t *testing.T) {
	fake := clock.NewFake(time.Now())
	health, listener, httpServer := harness(t)

	service, err := buildService(health, httpServer, listener, discardLogger(), fake)
	if err != nil {
		t.Fatalf("buildService: %v", err)
	}

	runResult := make(chan error, 1)
	go func() { runResult <- service.Run(context.Background()) }()

	waitFor(t, "server to start serving", func() bool { return httpServer.Serving() })
	if report := health.Readiness(context.Background()); !report.OK {
		t.Fatal("not ready before any drain began")
	}

	shutdownDone := make(chan error, 1)
	go func() { shutdownDone <- service.Shutdown(context.Background()) }()

	// The drain hook has run when readiness starts failing.
	waitFor(t, "readiness to fail", func() bool {
		return !health.Readiness(context.Background()).OK
	})

	// THE ASSERTION THIS TEST EXISTS FOR. Readiness is already failing, and the
	// listener must still answer, because the load balancer has not necessarily
	// noticed yet.
	if !httpServer.Serving() {
		t.Fatal("listener stopped before the propagation window elapsed")
	}
	if status, err := get(listener.Addr().String()); err != nil {
		t.Fatalf("request during drain failed: %v", err)
	} else if status != http.StatusOK {
		t.Fatalf("request during drain = %d, want 200", status)
	}

	// Only now let the propagation window pass. Advancing in steps rather than
	// one jump so the drain's timer fires before the shutdown budget expires,
	// regardless of how many timers the coordinator itself holds.
	deadline := time.Now().Add(10 * time.Second)
	for {
		select {
		case err := <-runResult:
			if err != nil {
				t.Fatalf("run: %v", err)
			}
			if httpServer.Serving() {
				t.Error("run returned while the server was still serving")
			}
			if err := <-shutdownDone; err != nil {
				t.Errorf("shutdown: %v", err)
			}
			return
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("service did not finish after the propagation window elapsed")
		}
		_ = fake.Advance(time.Second)
		time.Sleep(time.Millisecond)
	}
}

// Both components must be registered. The drain gate is what fails readiness
// and the HTTP component is what serves, so losing either silently removes half
// the shutdown sequence — and the process would still start and exit cleanly.
//
// Deliberately NOT an assertion about registration order: servicekit runs every
// Drain before any Stop (libs/go/servicekit, TestServiceDrainsBeforeCanceling-
// RunContext), and neither component contends with the other inside a phase, so
// pinning their order here would assert a fact the behavior does not rest on.
func TestBuildServiceRegistersBothComponents(t *testing.T) {
	health, listener, httpServer := harness(t)

	service, err := buildService(health, httpServer, listener, discardLogger(), clock.RealClock{})
	if err != nil {
		t.Fatalf("buildService: %v", err)
	}

	registered := make(map[string]bool)
	for _, name := range service.Components() {
		registered[name] = true
	}
	for _, want := range []string{"readiness-drain", "http"} {
		if !registered[want] {
			t.Errorf("component %q is not registered; got %v", want, service.Components())
		}
	}
}

func harness(t *testing.T) (*server.Health, net.Listener, *libhttpx.Server) {
	t.Helper()

	// A nil database is the embed role, which registers no readiness probe and
	// is ready whenever it is not draining — exactly the signal this test reads.
	health, err := server.NewHealth(nil)
	if err != nil {
		t.Fatalf("NewHealth: %v", err)
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	httpServer, err := libhttpx.NewServer(handler, libhttpx.ServerConfig{Addr: listener.Addr().String()})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	return health, listener, httpServer
}

func get(address string) (int, error) {
	response, err := http.Get("http://" + address + "/")
	if err != nil {
		return 0, err
	}
	defer func() { _ = response.Body.Close() }()
	_, _ = io.Copy(io.Discard, response.Body)
	return response.StatusCode, nil
}

func waitFor(t *testing.T, what string, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
