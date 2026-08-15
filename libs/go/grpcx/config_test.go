// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package grpcx

import (
	"testing"
	"time"

	"google.golang.org/grpc/keepalive"
)

func TestClientSecurityIsExplicit(t *testing.T) {
	if _, err := NewClient("dns:///service", ClientConfig{}); err == nil {
		t.Fatal("expected credentials error")
	}
	if _, err := NewClient("dns:///service", ClientConfig{Insecure: true, DefaultServiceConfig: "{"}); err == nil {
		t.Fatal("expected invalid service config")
	}
	connection, err := NewClient("dns:///service", ClientConfig{Insecure: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := connection.Close(); err != nil {
		t.Fatal(err)
	}
}
func TestServerConfig(t *testing.T) {
	if _, err := ServerOptions(ServerConfig{MaxReceiveBytes: -1}); err == nil {
		t.Fatal("expected invalid size")
	}
	if _, err := NewServer(ServerConfig{}); err != nil {
		t.Fatal(err)
	}
}

func TestConfigRejectsInvalidKeepaliveAndServiceConfigShape(t *testing.T) {
	if _, err := ServerOptions(ServerConfig{KeepaliveParameters: keepalive.ServerParameters{Time: -time.Second}}); err == nil {
		t.Fatal("expected invalid server keepalive")
	}
	if _, err := NewClient("dns:///service", ClientConfig{Insecure: true, KeepaliveParameters: keepalive.ClientParameters{Timeout: -time.Second}}); err == nil {
		t.Fatal("expected invalid client keepalive")
	}
	if _, err := NewClient("dns:///service", ClientConfig{Insecure: true, DefaultServiceConfig: `"scalar"`}); err == nil {
		t.Fatal("expected non-object service config error")
	}
}
