// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

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
