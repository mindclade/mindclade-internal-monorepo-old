// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package signing

import (
	"bytes"
	"context"
	"strings"
	"testing"

	mcclock "go.mindclade.dev/libs/go/clock"
)

// TestPreimageSeparatesDomainsUnderOneKey is the property the separator exists
// for: one key, one payload, two purposes, and the signature must not travel
// between them.
func TestPreimageSeparatesDomainsUnderOneKey(t *testing.T) {
	keyID := MustParseKeyID("shared/primary")
	secret := []byte("01234567890123456789012345678901")
	signer, err := NewHMACSigner(keyID, secret)
	if err != nil {
		t.Fatal(err)
	}
	set, err := NewKeySet(mcclock.RealClock{}, VerificationKey{ID: keyID, Algorithm: AlgorithmHMACSHA256, HMACKey: secret})
	if err != nil {
		t.Fatal(err)
	}

	payload := []byte("identical bytes in both domains")
	cursor := MustParseDomain("pagination-cursor/v1")
	ticket := MustParseDomain("execution-ticket-claims")

	minted, err := Preimage(cursor, payload)
	if err != nil {
		t.Fatal(err)
	}
	signature, err := signer.Sign(context.Background(), minted)
	if err != nil {
		t.Fatal(err)
	}
	if err := set.Verify(context.Background(), minted, signature); err != nil {
		t.Fatalf("signature must verify in the domain that minted it: %v", err)
	}

	foreign, err := Preimage(ticket, payload)
	if err != nil {
		t.Fatal(err)
	}
	if err := set.Verify(context.Background(), foreign, signature); err == nil {
		t.Fatal("a signature minted for the cursor domain verified as an execution ticket")
	}
}

// TestPreimageFramingIsInjective guards the reason the label is NUL terminated.
// Splitting the same bytes differently between domain and payload must not
// produce the same preimage, or an attacker who controls payload bytes could
// imitate another domain by absorbing the boundary.
func TestPreimageFramingIsInjective(t *testing.T) {
	first, err := Preimage(MustParseDomain("cursor"), []byte("/v1\x00payload"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Preimage(MustParseDomain("cursor/v1"), []byte("payload"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(first, second) {
		t.Fatal("distinct domain/payload splits produced identical preimages")
	}
}

// TestPreimageMatchesCanonicalClaimFraming pins the byte layout against the
// framing that control/runtime_authority and control/evidence already emit and
// that libs/rust/worker_protocol mirrors. A drift here would silently split the
// Go and Rust views of what a domain-separated document looks like.
func TestPreimageMatchesCanonicalClaimFraming(t *testing.T) {
	preimage, err := Preimage(MustParseDomain("execution-ticket-claims"), []byte("fields"))
	if err != nil {
		t.Fatal(err)
	}
	want := append([]byte("MCCE1/execution-ticket-claims"), 0x00)
	want = append(want, []byte("fields")...)
	if !bytes.Equal(preimage, want) {
		t.Fatalf("preimage framing drifted: got %q want %q", preimage, want)
	}
}

func TestParseDomainRejectsUnsafeLabels(t *testing.T) {
	for name, value := range map[string]string{
		"empty":          "",
		"whitespace":     "   ",
		"untrimmed":      " cursor ",
		"embedded NUL":   "cursor\x00v1",
		"uppercase":      "Cursor",
		"leading dash":   "-cursor",
		"leading slash":  "/cursor",
		"space inside":   "page cursor",
		"over the bound": strings.Repeat("a", MaximumDomainLength+1),
	} {
		if _, err := ParseDomain(value); err == nil {
			t.Fatalf("ParseDomain accepted an unsafe label (%s)", name)
		}
	}
	if _, err := ParseDomain("pagination-cursor/v1"); err != nil {
		t.Fatalf("ParseDomain rejected a canonical label: %v", err)
	}
}

// TestPreimageIsBounded checks that an oversized payload is refused rather than
// allocated, so a hostile length cannot turn a signing call into a large
// allocation.
func TestPreimageIsBounded(t *testing.T) {
	if _, err := Preimage(MustParseDomain("cursor/v1"), make([]byte, MaximumPreimageBytes)); err == nil {
		t.Fatal("Preimage accepted a payload past the bound")
	}
	if _, err := Preimage(MustParseDomain("cursor/v1"), make([]byte, 1024)); err != nil {
		t.Fatalf("Preimage refused a payload well inside the bound: %v", err)
	}
}
