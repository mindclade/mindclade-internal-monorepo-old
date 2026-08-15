// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package connectx

import (
	"net/http"
	"testing"

	"mindclade.internal/libs/go/faults"
)

func TestMountConvertsMuxPanic(t *testing.T) {
	mux := http.NewServeMux()
	if err := Mount(mux, "/mindclade.test.v1.TestService/Get", http.NotFoundHandler()); err != nil {
		t.Fatal(err)
	}
	err := Mount(mux, "/mindclade.test.v1.TestService/Get", http.NotFoundHandler())
	if faults.CodeOf(err) != faults.CodeFailedPrecondition {
		t.Fatalf("code = %s, err=%v", faults.CodeOf(err), err)
	}
	if faults.ReasonOf(err) != "connect_mount_failed" {
		t.Fatalf("reason = %q", faults.ReasonOf(err))
	}
}

func TestMountRejectsTypedNilHandler(t *testing.T) {
	var handler http.HandlerFunc
	err := Mount(http.NewServeMux(), "/service/method", handler)
	if faults.CodeOf(err) != faults.CodeInvalidArgument {
		t.Fatalf("code = %s", faults.CodeOf(err))
	}
}
