// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

package api

import (
	"testing"

	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/protocols/servicepolicy"
)

func TestEveryPromotedGRPCMethodHasExactAuthorizationMapping(t *testing.T) {
	for method, policy := range servicepolicy.All() {
		target, mapped, err := resolveGRPCAuthorization(method, nil)
		if err != nil || !mapped {
			t.Fatalf("%s mapping = (%#v, %t, %v)", method, target, mapped, err)
		}
		if target.Permission != policy.Permission || target.Resource.Type() != policy.ResourceType {
			t.Fatalf("%s mapping drifted: target=%#v policy=%#v", method, target, policy)
		}
	}
}

func TestGRPCAuthorizationPublicExceptionsAndUnknownFailClosed(t *testing.T) {
	for _, method := range []string{
		"/grpc.health.v1.Health/Check",
		"/grpc.reflection.v1.ServerReflection/ServerReflectionInfo",
	} {
		if _, mapped, err := resolveGRPCAuthorization(method, nil); err != nil || mapped {
			t.Fatalf("public %s = (%t, %v)", method, mapped, err)
		}
	}
	if _, mapped, err := resolveGRPCAuthorization("/unknown.v1.Service/Method", nil); mapped ||
		!faults.IsReason(err, "authorization_mapping_missing") {
		t.Fatalf("unknown mapping = (%t, %v)", mapped, err)
	}
}
