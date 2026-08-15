// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package blob

import "testing"

func TestKeyValidation(t *testing.T) {
	valid := []string{"artifacts/run/output.cif", "models/clade-1/checkpoint-0001"}
	for _, value := range valid {
		if _, err := ParseKey(value); err != nil {
			t.Fatalf("ParseKey(%q): %v", value, err)
		}
	}
	invalid := []string{"", "/leading", "trailing/", "a//b", "a/../b", " a", "a\\b"}
	for _, value := range invalid {
		if _, err := ParseKey(value); err == nil {
			t.Fatalf("ParseKey(%q) unexpectedly succeeded", value)
		}
	}
}
