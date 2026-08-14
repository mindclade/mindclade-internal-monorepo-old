// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

package connecttest

import (
	"connectrpc.com/connect"
	"testing"
)

func TestRecorder(t *testing.T) {
	recorder := &Recorder{}
	recorder.record(connect.Spec{Procedure: "/test.Service/Get", IsClient: true})
	calls := recorder.Calls()
	if len(calls) != 1 || calls[0].Procedure != "/test.Service/Get" || !calls[0].Client {
		t.Fatalf("calls=%#v", calls)
	}
}
