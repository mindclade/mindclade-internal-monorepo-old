// Copyright 2026 Mindclade. All rights reserved.
// Confidential and proprietary.

package gcs

import "testing"

func TestPrefixValidation(t *testing.T) {
	store := &Store{}
	if err := WithPrefix("tenant/artifacts/")(store); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"/bad/", "bad//prefix/", "../bad"} {
		if err := WithPrefix(value)(store); err == nil {
			t.Fatalf("prefix %q accepted", value)
		}
	}
}
