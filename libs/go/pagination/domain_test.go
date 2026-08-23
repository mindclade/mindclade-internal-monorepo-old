// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package pagination

import (
	"context"
	"testing"
	"time"

	mcclock "go.mindclade.dev/libs/go/clock"
	"go.mindclade.dev/libs/go/identifiers"
	"go.mindclade.dev/libs/go/signing"
)

// TestCursorSignatureDoesNotVerifyAcrossDomains is the regression test for the
// defect this domain separation exists to close.
//
// The control plane signs page tokens with the same HMAC key it uses for
// execution tickets, admission grants, route snapshots, revocation snapshots,
// and evidence claims. Before page tokens committed to a purpose, a cursor
// signature was a statement about bytes and nothing more; it stayed confined to
// pagination only because the other domains happen to begin their payloads with
// a different prefix. That is an accident of encoding, enforced nowhere, and
// the cursor payload is the one an attacker most directly influences.
//
// Both codecs below share one key and differ only in domain. A token minted by
// one must be cryptographically rejected by the other. With the separator
// removed this test fails: the payload bytes are identical, so the MAC matches
// and the token crosses the boundary.
func TestCursorSignatureDoesNotVerifyAcrossDomains(t *testing.T) {
	now := time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC)
	clock := mcclock.NewFake(now)
	keyID := signing.MustParseKeyID("shared/primary")
	secret := []byte("01234567890123456789012345678901")
	signer, err := signing.NewHMACSigner(keyID, secret)
	if err != nil {
		t.Fatal(err)
	}
	keys, err := signing.NewKeySet(clock, signing.VerificationKey{ID: keyID, Algorithm: signing.AlgorithmHMACSHA256, HMACKey: secret})
	if err != nil {
		t.Fatal(err)
	}

	cursors, err := NewCodec(signer, keys, CursorDomain, clock, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	// A second purpose backed by the identical key, standing in for any other
	// document class this signer is trusted for.
	other, err := NewCodec(signer, keys, signing.MustParseDomain("execution-ticket-claims"), clock, time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	binding := Binding{Scope: "tenant/acme", Resource: "runs", FilterDigest: identifiers.SHA256String("state=running"), Order: []Order{{Field: "created_at", Direction: Descending}}}
	token, err := cursors.Encode(context.Background(), binding, "run_01")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cursors.Decode(context.Background(), token, binding); err != nil {
		t.Fatalf("token must remain valid in the domain that minted it: %v", err)
	}
	if _, err := other.Decode(context.Background(), token, binding); err == nil {
		t.Fatal("a page-token signature verified under a different domain using the same key")
	}
}

// TestCodecRequiresDomain pins the fail-closed property: a codec cannot be
// built without naming a purpose. An optional separator that a call site can
// forget provides no separation at all, so the zero value must be refused
// rather than quietly defaulting.
func TestCodecRequiresDomain(t *testing.T) {
	clock := mcclock.NewFake(time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC))
	keyID := signing.MustParseKeyID("shared/primary")
	secret := []byte("01234567890123456789012345678901")
	signer, err := signing.NewHMACSigner(keyID, secret)
	if err != nil {
		t.Fatal(err)
	}
	keys, err := signing.NewKeySet(clock, signing.VerificationKey{ID: keyID, Algorithm: signing.AlgorithmHMACSHA256, HMACKey: secret})
	if err != nil {
		t.Fatal(err)
	}
	for name, domain := range map[string]signing.Domain{
		"empty":            "",
		"whitespace":       "  ",
		"embedded NUL":     signing.Domain("cursor\x00v1"),
		"uppercase":        "Pagination-Cursor/v1",
		"leading operator": "/cursor",
	} {
		if _, err := NewCodec(signer, keys, domain, clock, time.Hour); err == nil {
			t.Fatalf("codec accepted an invalid domain (%s)", name)
		}
	}
}
