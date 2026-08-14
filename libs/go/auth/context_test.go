// Copyright 2026 Mindclade. All rights reserved.
// Confidential and proprietary.

package auth

import (
	"context"
	"errors"
	"testing"
)

func TestPrincipalContext(t *testing.T) {
	t.Parallel()

	principal := testPrincipal(t)
	ctx, err := WithPrincipal(context.Background(), principal)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := RequirePrincipal(ctx)
	if err != nil || resolved.Key() != principal.Key() {
		t.Fatalf("RequirePrincipal() = %s, %v", resolved.Key(), err)
	}
	if _, err := RequirePrincipal(context.Background()); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("missing principal error = %v", err)
	}
}
