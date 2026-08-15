// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package blob

import (
	"testing"

	"mindclade.internal/libs/go/identifiers"
)

func TestAttributesClone(t *testing.T) {
	attributes := Attributes{Key: MustParseKey("a/b"), Size: 3, Generation: 1, Digest: identifiers.SHA256([]byte("abc")), Metadata: Metadata{"schema": "1"}}
	clone := attributes.Clone()
	clone.Metadata["schema"] = "2"
	if attributes.Metadata["schema"] != "1" {
		t.Fatal("Clone aliased metadata")
	}
	if err := attributes.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestDeleteOptionsRejectIfNotExists(t *testing.T) {
	options := DeleteOptions{Preconditions: Preconditions{IfNotExists: true}}
	if err := options.Validate(); err == nil {
		t.Fatal("DeleteOptions.Validate() returned nil")
	}
}
