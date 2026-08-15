// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package connectx

import "testing"

func TestOptionsValidation(t *testing.T) {
	if _, err := HandlerOptions(HandlerConfig{ReadMaxBytes: -1}); err == nil {
		t.Fatal("expected invalid handler limit")
	}
	if _, err := ClientOptions(ClientConfig{Protocol: Protocol("smtp")}); err == nil {
		t.Fatal("expected invalid protocol")
	}
	if _, err := ClientOptions(ClientConfig{Protocol: ProtocolGRPC, EnableHTTPGet: true}); err == nil {
		t.Fatal("expected GET/protocol conflict")
	}
	if options, err := HandlerOptions(HandlerConfig{ReadMaxBytes: 1024, SendMaxBytes: 2048}); err != nil || len(options) != 2 {
		t.Fatalf("options=%d err=%v", len(options), err)
	}
	if got, err := NormalizeBaseURL("https://api.mindclade.dev/"); err != nil || got != "https://api.mindclade.dev" {
		t.Fatalf("got=%q err=%v", got, err)
	}
}

func TestOperationForProcedure(t *testing.T) {
	operation, err := OperationForProcedure("/mindclade.runs.v1.RunsService/CreateRun")
	if err != nil {
		t.Fatal(err)
	}
	if operation.String() != "rpc.mindclade.runs.v1.RunsService.CreateRun" {
		t.Fatalf("operation = %q", operation.String())
	}
	if _, err := OperationForProcedure("invalid"); err == nil {
		t.Fatal("expected invalid procedure")
	}
}
