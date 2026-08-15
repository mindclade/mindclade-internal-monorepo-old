// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package reflection

import (
	"testing"

	"google.golang.org/grpc"

	"mindclade.internal/libs/go/faults"
)

func TestRegister(t *testing.T) {
	if err := Register(nil, Config{}); faults.CodeOf(err) != faults.CodeInvalidArgument {
		t.Fatalf("nil code = %s", faults.CodeOf(err))
	}
	server := grpc.NewServer()
	if err := Register(server, Config{V1Only: true}); err != nil {
		t.Fatal(err)
	}
}
