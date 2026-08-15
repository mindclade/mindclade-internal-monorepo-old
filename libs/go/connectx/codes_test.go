// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package connectx

import (
	"testing"

	"connectrpc.com/connect"

	"go.mindclade.dev/libs/go/faults"
)

func TestCodeRoundTrip(t *testing.T) {
	codes := []faults.Code{
		faults.CodeCanceled, faults.CodeInvalidArgument, faults.CodeDeadlineExceeded,
		faults.CodeNotFound, faults.CodeAlreadyExists, faults.CodePermissionDenied,
		faults.CodeUnauthenticated, faults.CodeResourceExhausted,
		faults.CodeFailedPrecondition, faults.CodeAborted, faults.CodeOutOfRange,
		faults.CodeNotImplemented, faults.CodeInternal, faults.CodeUnavailable,
		faults.CodeDataLoss,
	}
	for _, code := range codes {
		if got := FaultCode(CodeFromFault(code)); got != code {
			t.Fatalf("code %s round-tripped as %s", code, got)
		}
	}
	if CodeFromFault(faults.CodeConflict) != connect.CodeAborted {
		t.Fatal("conflict must map to aborted")
	}
}
