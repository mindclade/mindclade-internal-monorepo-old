// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package httpx

import (
	"context"
	"errors"
	"net"
	"net/http"
	"sync"

	"go.mindclade.dev/libs/go/faults"
	"testing"
	"time"
)

type notifyingListener struct {
	net.Listener
	accepting chan struct{}
	once      sync.Once
}

func (listener *notifyingListener) Accept() (net.Conn, error) {
	listener.once.Do(func() { close(listener.accepting) })
	return listener.Listener.Accept()
}

func TestServerServeAndShutdown(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server, err := NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }), ServerConfig{})
	if err != nil {
		t.Fatal(err)
	}
	notifying := &notifyingListener{Listener: listener, accepting: make(chan struct{})}
	done := make(chan error, 1)
	go func() { done <- server.Serve(notifying) }()
	<-notifying.accepting
	if !server.Serving() {
		t.Fatal("server did not start")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestServerShutdownBeforeServePreventsLateStart(t *testing.T) {
	server, err := NewServer(http.NotFoundHandler(), ServerConfig{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	if err := server.Serve(failingListener{err: errors.New("accept must not run")}); err != nil {
		t.Fatalf("Serve after Shutdown = %v", err)
	}
	if server.Serving() {
		t.Fatal("server started after shutdown")
	}
}

func TestServerRejectsTypedNilHandler(t *testing.T) {
	var handler http.HandlerFunc
	if _, err := NewServer(handler, ServerConfig{}); err == nil {
		t.Fatal("expected typed nil handler error")
	}
}

type failingListener struct{ err error }

func (listener failingListener) Accept() (net.Conn, error) { return nil, listener.err }
func (failingListener) Close() error                       { return nil }
func (failingListener) Addr() net.Addr                     { return testAddress("failing") }

type testAddress string

func (address testAddress) Network() string { return "test" }
func (address testAddress) String() string  { return string(address) }

func TestServerIsSingleUse(t *testing.T) {
	server, err := NewServer(http.NotFoundHandler(), ServerConfig{})
	if err != nil {
		t.Fatal(err)
	}
	first := server.Serve(failingListener{err: errors.New("accept failed")})
	if faults.CodeOf(first) != faults.CodeUnavailable {
		t.Fatalf("first code = %s", faults.CodeOf(first))
	}
	second := server.Serve(failingListener{err: errors.New("accept failed again")})
	if faults.CodeOf(second) != faults.CodeFailedPrecondition {
		t.Fatalf("second code = %s", faults.CodeOf(second))
	}
	if faults.ReasonOf(second) != "http_server_already_started" {
		t.Fatalf("reason = %q", faults.ReasonOf(second))
	}
}
