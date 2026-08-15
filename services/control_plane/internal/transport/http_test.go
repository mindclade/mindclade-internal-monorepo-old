// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package transport

import (
	"net"
	"net/http"
	"testing"

	"go.mindclade.dev/libs/go/httpx"
	"go.mindclade.dev/libs/go/servicekit/production"
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
