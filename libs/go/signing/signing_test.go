// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package signing

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"testing"
	"time"

	mcclock "go.mindclade.dev/libs/go/clock"
)

func TestHMACRoundTripAndText(t *testing.T) {
	keyID := MustParseKeyID("cursor/2026-08")
	secret := []byte("01234567890123456789012345678901")
	signer, err := NewHMACSigner(keyID, secret)
	if err != nil {
		t.Fatal(err)
	}
	signature, err := signer.Sign(context.Background(), []byte("payload"))
	if err != nil {
		t.Fatal(err)
	}
	text, err := signature.MarshalText()
	if err != nil {
		t.Fatal(err)
	}
	var decoded Signature
	if err := decoded.UnmarshalText(text); err != nil {
		t.Fatal(err)
	}
	set, err := NewKeySet(mcclock.RealClock{}, VerificationKey{ID: keyID, Algorithm: AlgorithmHMACSHA256, HMACKey: secret})
	if err != nil {
		t.Fatal(err)
	}
	if err := set.Verify(context.Background(), []byte("payload"), decoded); err != nil {
		t.Fatal(err)
	}
	if err := set.Verify(context.Background(), []byte("tampered"), decoded); err == nil {
		t.Fatal("tampered payload verified")
	}
	encoded, err := json.Marshal(signature)
	if err != nil {
		t.Fatal(err)
	}
	var jsonDecoded Signature
	if err := json.Unmarshal(encoded, &jsonDecoded); err != nil {
		t.Fatal(err)
	}
	if err := set.Verify(context.Background(), []byte("payload"), jsonDecoded); err != nil {
		t.Fatal(err)
	}
}
func TestEd25519ValidityWindow(t *testing.T) {
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	keyID := MustParseKeyID("runtime/primary")
	signer, err := NewEd25519Signer(keyID, private)
	if err != nil {
		t.Fatal(err)
	}
	signature, err := signer.Sign(context.Background(), []byte("ticket"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC)
	fake := mcclock.NewFake(now)
	set, err := NewKeySet(fake, VerificationKey{ID: keyID, Algorithm: AlgorithmEd25519, Ed25519PublicKey: public, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if err := set.Verify(context.Background(), []byte("ticket"), signature); err != nil {
		t.Fatal(err)
	}
	if err := fake.Advance(2 * time.Hour); err != nil {
		t.Fatal(err)
	}
	if err := set.Verify(context.Background(), []byte("ticket"), signature); err == nil {
		t.Fatal("expired key verified")
	}
}
