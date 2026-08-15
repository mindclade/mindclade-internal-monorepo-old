// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package gcs

import "testing"

func TestNormalizePrefix(t *testing.T) {
	t.Parallel()
	for input, expected := range map[string]string{"": "", "artifacts": "artifacts/", "runs/data": "runs/data/", "runs/data/": "runs/data/"} {
		actual, err := normalizePrefix(input)
		if err != nil || actual != expected {
			t.Fatalf("normalizePrefix(%q) = %q, %v", input, actual, err)
		}
	}
	for _, input := range []string{"/absolute", "a//b", "../escape", "a\\b", " spaced "} {
		if _, err := normalizePrefix(input); err == nil {
			t.Fatalf("normalizePrefix(%q) unexpectedly succeeded", input)
		}
	}
}
