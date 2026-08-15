// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package auth

import "testing"

func TestPermissionSetWildcardMatching(t *testing.T) {
	t.Parallel()

	set, err := NewPermissionSet(
		MustParsePermission("runs.read"),
		MustParsePermission("models.release.*"),
	)
	if err != nil {
		t.Fatal(err)
	}
	for _, permission := range []Permission{
		MustParsePermission("runs.read"),
		MustParsePermission("models.release.promote"),
	} {
		if !set.Allows(permission) {
			t.Fatalf("permission %q was not allowed", permission)
		}
	}
	if set.Allows(MustParsePermission("runs.delete")) {
		t.Fatal("ungranted permission allowed")
	}
	if set.Allows(MustParsePermission("models.*")) {
		t.Fatal("wildcard request allowed")
	}
}

func TestPermissionValidation(t *testing.T) {
	t.Parallel()

	for _, value := range []string{"", "Runs.Read", "runs..read", "runs.*.delete"} {
		if _, err := ParsePermission(value); err == nil {
			t.Fatalf("ParsePermission(%q) succeeded", value)
		}
	}
}
