// Copyright 2026 Mindclade. All rights reserved.
// Confidential and proprietary.

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
