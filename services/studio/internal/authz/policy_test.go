// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package authz

import (
	"context"
	"errors"
	"testing"

	"go.mindclade.dev/services/studio/internal/iap"
)

func TestPolicyAllowsOnlyExactStableSubjects(t *testing.T) {
	policy, err := New("accounts.google.com:1001, accounts.google.com:2002")
	if err != nil {
		t.Fatal(err)
	}

	if err := policy.Resolve(context.Background(), iap.Assertion{
		Subject: "accounts.google.com:1001",
		Email:   "renamed@example.com",
	}); err != nil {
		t.Fatalf("configured subject denied: %v", err)
	}
	for _, assertion := range []iap.Assertion{
		{Subject: "accounts.google.com:100", Email: "allowed@example.com"},
		{Subject: "accounts.google.com:9999", Email: "allowed@example.com"},
		{Email: "allowed@example.com"},
	} {
		if err := policy.Resolve(context.Background(), assertion); !errors.Is(err, ErrDenied) {
			t.Fatalf("assertion %+v: error = %v, want ErrDenied", assertion, err)
		}
	}
}

func TestPolicyFailsClosedWhenConfigurationIsEmptyOrInvalid(t *testing.T) {
	for _, value := range []string{
		"", " , ", "person@example.com", "accounts.google.com:",
		"accounts.google.com:１２", "accounts.google.com:1,accounts.google.com:1",
	} {
		if policy, err := New(value); err == nil || policy != nil {
			t.Fatalf("New(%q) = %#v, %v; want construction failure", value, policy, err)
		}
	}

	var policy *Policy
	if err := policy.Resolve(context.Background(), iap.Assertion{Subject: "accounts.google.com:1"}); !errors.Is(err, ErrDenied) {
		t.Fatalf("nil policy error = %v, want ErrDenied", err)
	}
}

func TestPolicyHonorsCancellation(t *testing.T) {
	policy, err := New("accounts.google.com:1001")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := policy.Resolve(ctx, iap.Assertion{Subject: "accounts.google.com:1001"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}
