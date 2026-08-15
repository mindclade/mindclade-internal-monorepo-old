// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package httpx

import (
	"net/http"
	"testing"
	"time"
)

func TestNewClientUsesIndependentTransport(t *testing.T) {
	client, err := NewClient(ClientConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if client.Timeout != DefaultClientTimeout {
		t.Fatalf("timeout = %v", client.Timeout)
	}
	if client.Transport == http.DefaultTransport {
		t.Fatal("mutated or reused default transport")
	}
	metadataTransport, ok := client.Transport.(RequestMetadataTransport)
	if !ok || metadataTransport.Base == nil || metadataTransport.Base == http.DefaultTransport {
		t.Fatalf("transport = %T", client.Transport)
	}
}

func TestInvalidClientConfig(t *testing.T) {
	if _, err := NewClient(ClientConfig{Timeout: -time.Second}); err == nil {
		t.Fatal("expected error")
	}
}
