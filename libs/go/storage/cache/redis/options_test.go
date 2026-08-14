// Copyright 2026 Mindclade. All rights reserved.
// Confidential and proprietary.

package redis

import "testing"

func TestOptions(t *testing.T) {
	store := &Store{}
	if err := WithPrefix("mindclade:test:")(store); err != nil || store.prefix != "mindclade:test:" {
		t.Fatalf("WithPrefix() = %v, prefix=%q", err, store.prefix)
	}
	if err := WithMaximumEntryBytes(1024)(store); err != nil || store.maximumEntryBytes != 1024 {
		t.Fatalf("WithMaximumEntryBytes() = %v", err)
	}
	for _, value := range []string{"", " bad", "bad\n"} {
		if err := WithPrefix(value)(store); err == nil {
			t.Fatalf("WithPrefix(%q) returned nil", value)
		}
	}
}
