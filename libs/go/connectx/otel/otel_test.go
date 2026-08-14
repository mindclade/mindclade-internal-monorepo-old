// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

package otel

import "testing"

func TestNewInterceptor(t *testing.T) {
	interceptor, err := NewInterceptor()
	if err != nil {
		t.Fatal(err)
	}
	if interceptor == nil {
		t.Fatal("expected interceptor")
	}
}
