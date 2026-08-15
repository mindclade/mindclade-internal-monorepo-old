// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

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
