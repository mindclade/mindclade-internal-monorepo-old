// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package cache

import "testing"

func TestKeyAndEntry(t *testing.T) {
	key := MustParseKey("tenant:run:1")
	entry := Entry{Key: key, Value: []byte("x"), Version: 1}
	if err := entry.Validate(); err != nil {
		t.Fatal(err)
	}
	clone := entry.Clone()
	clone.Value[0] = 'y'
	if string(entry.Value) != "x" {
		t.Fatal("clone aliased value")
	}
}
