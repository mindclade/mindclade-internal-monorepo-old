// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

package grpcx

import (
	"errors"
	"net"
	"testing"

	"mindclade.internal/libs/go/faults"
)

type failingListener struct{ err error }

func (listener failingListener) Accept() (net.Conn, error) { return nil, listener.err }
func (failingListener) Close() error                       { return nil }
func (failingListener) Addr() net.Addr                     { return testAddress("failing") }

type testAddress string

func (address testAddress) Network() string { return "test" }
func (address testAddress) String() string  { return string(address) }

func TestServerIsSingleUse(t *testing.T) {
	server, err := NewServer(ServerConfig{})
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
	if faults.ReasonOf(second) != "grpc_server_already_started" {
		t.Fatalf("reason = %q", faults.ReasonOf(second))
	}
}
