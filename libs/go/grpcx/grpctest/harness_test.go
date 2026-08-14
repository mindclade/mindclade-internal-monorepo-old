// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

package grpctest

import (
	"context"
	"testing"

	"google.golang.org/grpc"
)

func TestHarnessClient(t *testing.T) {
	harness := Start(t, grpc.NewServer())
	connection, err := harness.Client(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if connection == nil {
		t.Fatal("nil client connection")
	}
	if err := connection.Close(); err != nil {
		t.Fatal(err)
	}
}
