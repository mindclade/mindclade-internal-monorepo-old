// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

package transport

import (
	"net"
	"net/http"
	"testing"

	"mindclade.internal/libs/go/httpx"
	"mindclade.internal/libs/go/servicekit/production"
)

func TestHTTPMechanism(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	adapter, err := NewHTTP("http", http.NotFoundHandler(), listener, httpx.ServerConfig{})
	if err != nil {
		t.Fatal(err)
	}
	mechanism, err := adapter.Mechanism()
	if err != nil {
		t.Fatal(err)
	}
	if mechanism.Capability != production.CapabilityHTTP || mechanism.Component.Name != "http" {
		t.Fatalf("mechanism=%+v", mechanism)
	}
}
