// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package grpcx

import (
	"context"
	"testing"

	"google.golang.org/grpc/metadata"

	"mindclade.internal/libs/go/faults"
	"mindclade.internal/libs/go/requestmeta"
)

func TestMetadataContextValidation(t *testing.T) {
	if _, _, err := ExtractIncoming(nil); faults.CodeOf(err) != faults.CodeInvalidArgument {
		t.Fatalf("extract code=%s err=%v", faults.CodeOf(err), err)
	}
	if _, err := InjectOutgoing(nil); faults.CodeOf(err) != faults.CodeInvalidArgument {
		t.Fatalf("inject code=%s err=%v", faults.CodeOf(err), err)
	}
}

func TestMetadataRoundTrip(t *testing.T) {
	requestID, err := requestmeta.NewRequestID()
	if err != nil {
		t.Fatal(err)
	}
	ctx, err := requestmeta.WithRequestID(context.Background(), requestID)
	if err != nil {
		t.Fatal(err)
	}
	outgoing, err := InjectOutgoing(ctx)
	if err != nil {
		t.Fatal(err)
	}
	values, ok := metadata.FromOutgoingContext(outgoing)
	if !ok || values.Get(requestmeta.PropagationKeyRequestID)[0] != requestID.String() {
		t.Fatalf("metadata=%v", values)
	}
}

func TestExtractIncomingRejectsAmbiguousLineage(t *testing.T) {
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs(
		requestmeta.PropagationKeyRequestID, "request_019c7af21b8276d2a0d522fe41739a21",
		requestmeta.PropagationKeyRequestID, "request_019c7af21b827f53a6b84710f1815c84",
	))
	if _, _, err := ExtractIncoming(ctx); faults.ReasonOf(err) != "ambiguous_request_metadata" {
		t.Fatalf("reason=%q err=%v", faults.ReasonOf(err), err)
	}
}

func TestInjectOutgoingGeneratesRequestID(t *testing.T) {
	outgoing, err := InjectOutgoing(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	values, ok := metadata.FromOutgoingContext(outgoing)
	if !ok || len(values.Get(requestmeta.PropagationKeyRequestID)) != 1 {
		t.Fatalf("metadata=%v", values)
	}
	if _, ok := requestmeta.RequestIDFromContext(outgoing); !ok {
		t.Fatal("outgoing context missing request ID")
	}
}
