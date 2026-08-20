// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.mindclade.dev/libs/go/faults"
)

func TestChainAuthenticator(t *testing.T) {
	t.Parallel()

	unsupported := AuthenticatorFunc(func(context.Context, Credential) (Principal, error) {
		return Principal{}, newFault(ErrUnsupportedCredential, faults.CodeUnauthenticated, "unsupported", "unsupported_credential", "test", nil)
	})
	principal := testPrincipal(t)
	success := AuthenticatorFunc(func(context.Context, Credential) (Principal, error) {
		return principal, nil
	})
	chain, err := NewChainAuthenticator(unsupported, success)
	if err != nil {
		t.Fatal(err)
	}
	credential, _ := Bearer("token")
	resolved, err := AuthenticateAt(context.Background(), chain, credential, time.Unix(150, 0), 0)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Key() != principal.Key() {
		t.Fatalf("principal = %s", resolved.Key())
	}
}

func TestAuthenticationPreservesProviderFailure(t *testing.T) {
	t.Parallel()

	provider := AuthenticatorFunc(func(context.Context, Credential) (Principal, error) {
		return Principal{}, faults.New(faults.CodeUnavailable, "identity provider unavailable", faults.WithReason("idp_unavailable"), faults.WithRetryPolicy(faults.BackoffRetry(3)))
	})
	credential, _ := Bearer("token")
	_, err := Authenticate(context.Background(), provider, credential)
	if !faults.IsCode(err, faults.CodeUnavailable) || !faults.IsReason(err, "idp_unavailable") || !faults.IsRetryable(err) {
		t.Fatalf("error = %v", err)
	}
	if errors.Is(err, ErrUnauthenticated) {
		t.Fatal("provider outage was mislabeled unauthenticated")
	}
}
