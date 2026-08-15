// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package idempotency

import (
	"encoding/json"
	"mindclade.internal/libs/go/faults"
	"testing"
)

func TestKeyScopeIdentity(t *testing.T) {
	key, err := ParseKey("request-123456")
	if err != nil {
		t.Fatal(err)
	}
	scope, err := ParseScope("control-plane/runs.create")
	if err != nil {
		t.Fatal(err)
	}
	identity, err := NewIdentity(scope, key)
	if err != nil {
		t.Fatal(err)
	}
	if !identity.Digest().Valid() {
		t.Fatal("missing identity digest")
	}
	encoded, err := json.Marshal(identity)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Identity
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded != identity {
		t.Fatalf("decoded=%#v", decoded)
	}
}
func TestInvalidKeyAndScope(t *testing.T) {
	if _, err := ParseKey("short"); !faults.IsReason(err, ReasonInvalidKey) {
		t.Fatalf("error=%v", err)
	}
	if _, err := ParseScope("Runs Create"); !faults.IsReason(err, ReasonInvalidScope) {
		t.Fatalf("error=%v", err)
	}
}
