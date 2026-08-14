// Copyright 2026 Mindclade. All rights reserved.
package pagination

import (
	"context"
	"testing"
	"time"

	mcclock "mindclade.internal/libs/go/clock"
	"mindclade.internal/libs/go/identifiers"
	"mindclade.internal/libs/go/signing"
)

func TestCursorRoundTripAndBinding(t *testing.T) {
	now := time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC)
	clock := mcclock.NewFake(now)
	keyID := signing.MustParseKeyID("pagination/primary")
	secret := []byte("01234567890123456789012345678901")
	signer, err := signing.NewHMACSigner(keyID, secret)
	if err != nil {
		t.Fatal(err)
	}
	keys, err := signing.NewKeySet(clock, signing.VerificationKey{ID: keyID, Algorithm: signing.AlgorithmHMACSHA256, HMACKey: secret})
	if err != nil {
		t.Fatal(err)
	}
	codec, err := NewCodec(signer, keys, clock, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	binding := Binding{Scope: "tenant/acme", Resource: "runs", FilterDigest: identifiers.SHA256String("state=running"), Order: []Order{{Field: "created_at", Direction: Descending}, {Field: "id", Direction: Ascending}}}
	token, err := codec.Encode(context.Background(), binding, "2026-08-13T11:00:00Z", "run_01")
	if err != nil {
		t.Fatal(err)
	}
	cursor, err := codec.Decode(context.Background(), token, binding)
	if err != nil {
		t.Fatal(err)
	}
	if len(cursor.Values) != 2 || cursor.Values[1] != "run_01" {
		t.Fatalf("cursor=%+v", cursor)
	}
	mismatch := binding
	mismatch.Resource = "jobs"
	if _, err := codec.Decode(context.Background(), token, mismatch); err == nil {
		t.Fatal("mismatched cursor accepted")
	}
	if err := clock.Advance(2 * time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, err := codec.Decode(context.Background(), token, binding); err == nil {
		t.Fatal("expired cursor accepted")
	}
}
func TestRequestBounds(t *testing.T) {
	if err := (Request{}).Validate(); err != nil {
		t.Fatal(err)
	}
	if err := (Request{PageSize: MaximumPageSize + 1}).Validate(); err == nil {
		t.Fatal("oversized page accepted")
	}
}
