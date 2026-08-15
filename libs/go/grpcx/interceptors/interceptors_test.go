// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package interceptors

import (
	"google.golang.org/grpc/metadata"
	"go.mindclade.dev/libs/go/auth"
	"go.mindclade.dev/libs/go/faults"
	"testing"
)

type message struct{ invalid bool }

func (value *message) Validate() error {
	if value.invalid {
		return faults.New(faults.CodeInvalidArgument, "invalid")
	}
	return nil
}
func TestValidateMessage(t *testing.T) {
	if err := validateMessage(&message{}); err != nil {
		t.Fatal(err)
	}
	if err := validateMessage(&message{invalid: true}); faults.CodeOf(err) != faults.CodeInvalidArgument {
		t.Fatalf("code=%s", faults.CodeOf(err))
	}
}
func TestCredentialExtractor(t *testing.T) {
	credential, present, err := DefaultCredentialExtractor(metadata.Pairs("authorization", "Bearer token"))
	if err != nil {
		t.Fatal(err)
	}
	if !present || credential.Scheme() != auth.CredentialSchemeBearer || string(credential.Value()) != "token" {
		t.Fatal("unexpected credential")
	}
}

func TestCredentialExtractorRejectsAmbiguousOrMalformedMetadata(t *testing.T) {
	cases := []metadata.MD{
		metadata.Pairs("authorization", "Bearer one", "authorization", "Bearer two"),
		metadata.Pairs("authorization", "Bearer token extra"),
		metadata.Pairs("authorization", "Basic token"),
	}
	for _, value := range cases {
		if _, _, err := DefaultCredentialExtractor(value); faults.CodeOf(err) != faults.CodeUnauthenticated {
			t.Fatalf("metadata=%v code=%s err=%v", value, faults.CodeOf(err), err)
		}
	}
}
