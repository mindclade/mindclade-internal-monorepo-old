// Copyright 2026 Mindclade. All rights reserved.
// Confidential and proprietary.

package auth

import (
	"strings"
	"testing"
)

func TestCredentialRedactionAndCopies(t *testing.T) {
	t.Parallel()

	input := []byte("super-secret-token")
	credential, err := NewCredential(CredentialSchemeBearer, input, WithCredentialAttributes(map[string]string{"key_id": "kid-1"}))
	if err != nil {
		t.Fatal(err)
	}
	input[0] = 'X'
	value := credential.Value()
	if string(value) != "super-secret-token" {
		t.Fatalf("Value() = %q", value)
	}
	value[0] = 'Y'
	if string(credential.Value()) != "super-secret-token" {
		t.Fatal("credential value was mutated through accessor")
	}
	if strings.Contains(credential.String(), "super-secret-token") || !strings.Contains(credential.String(), "REDACTED") {
		t.Fatalf("String() = %q", credential.String())
	}
}

func TestCredentialValidation(t *testing.T) {
	t.Parallel()

	if _, err := NewCredential(CredentialSchemeBearer, nil); err == nil {
		t.Fatal("empty credential accepted")
	}
	if _, err := ParseCredentialScheme("Bearer"); err != nil {
		t.Fatal(err)
	}
}
