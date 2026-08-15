// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package iap

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

const testAudience = "/projects/123456789012/global/backendServices/987654321"

type signer struct {
	kid string
	key *ecdsa.PrivateKey
}

func newSigner(t *testing.T, kid string) *signer {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	return &signer{kid: kid, key: k}
}

// mint builds a token. alg is overridable so the algorithm-confusion cases can
// be exercised.
func (s *signer) mint(t *testing.T, alg string, claims map[string]any) string {
	t.Helper()
	enc := func(v any) string {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		return base64.RawURLEncoding.EncodeToString(b)
	}

	head := enc(map[string]string{"alg": alg, "kid": s.kid})
	body := enc(claims)
	input := head + "." + body

	digest := sha256.Sum256([]byte(input))
	r, sv, err := ecdsa.Sign(rand.Reader, s.key, digest[:])
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	// Raw r||s, each left-padded to 32 bytes — NOT ASN.1 DER.
	sig := make([]byte, 64)
	r.FillBytes(sig[:32])
	sv.FillBytes(sig[32:])

	return input + "." + base64.RawURLEncoding.EncodeToString(sig)
}

func validClaims() map[string]any {
	return map[string]any{
		"iss":   Issuer,
		"sub":   "accounts.google.com:110000000000000000001",
		"email": "alice@mindclade.com",
		"aud":   testAudience,
		"iat":   time.Now().Add(-time.Minute).Unix(),
		"exp":   time.Now().Add(time.Hour).Unix(),
	}
}

// keyServer publishes the signers' public keys in IAP's JWK shape.
func keyServer(t *testing.T, signers ...*signer) *httptest.Server {
	t.Helper()
	type jwk struct {
		Kid string `json:"kid"`
		Crv string `json:"crv"`
		X   string `json:"x"`
		Y   string `json:"y"`
	}
	keys := make([]jwk, 0, len(signers))
	for _, s := range signers {
		x := make([]byte, 32)
		y := make([]byte, 32)
		s.key.X.FillBytes(x)
		s.key.Y.FillBytes(y)
		keys = append(keys, jwk{
			Kid: s.kid,
			Crv: "P-256",
			X:   base64.RawURLEncoding.EncodeToString(x),
			Y:   base64.RawURLEncoding.EncodeToString(y),
		})
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": keys})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func newTestVerifier(t *testing.T, signers ...*signer) *Verifier {
	t.Helper()
	v, err := NewVerifier(testAudience, nil)
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}
	v.keysURL = keyServer(t, signers...).URL
	return v
}

func TestVerifiesAGenuineAssertion(t *testing.T) {
	s := newSigner(t, "kid-1")
	v := newTestVerifier(t, s)

	got, err := v.Verify(context.Background(), s.mint(t, "ES256", validClaims()))
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if got.Subject != "accounts.google.com:110000000000000000001" {
		t.Errorf("Subject = %q", got.Subject)
	}
	if got.Email != "alice@mindclade.com" {
		t.Errorf("Email = %q", got.Email)
	}
}

// The acceptance check, verbatim: a request carrying a garbage assertion must
// be rejected. This is the test that proves the BFF VERIFIES the assertion
// rather than reading it.
func TestForgedAssertionIsRejected(t *testing.T) {
	s := newSigner(t, "kid-1")
	v := newTestVerifier(t, s)

	for _, token := range []string{
		"not.a.jwt",
		"",
		"a.b",
		"a.b.c.d",
		base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"ES256"}`)) + ".x.y",
	} {
		if _, err := v.Verify(context.Background(), token); err == nil {
			t.Errorf("Verify(%q) succeeded; want an error", token)
		}
	}
}

// An assertion signed by a key IAP does not publish must fail. Otherwise
// anyone can mint one.
func TestUnknownSignerIsRejected(t *testing.T) {
	published := newSigner(t, "kid-1")
	attacker := newSigner(t, "kid-attacker")
	v := newTestVerifier(t, published)

	_, err := v.Verify(context.Background(), attacker.mint(t, "ES256", validClaims()))
	if !errors.Is(err, ErrUnknownKey) {
		t.Fatalf("err = %v, want ErrUnknownKey", err)
	}
}

// An attacker who reuses a PUBLISHED kid but signs with their own key must
// fail at the signature, not merely at key lookup.
func TestWrongKeyForKnownKidIsRejected(t *testing.T) {
	published := newSigner(t, "kid-1")
	impostor := newSigner(t, "kid-1") // same kid, different key
	v := newTestVerifier(t, published)

	_, err := v.Verify(context.Background(), impostor.mint(t, "ES256", validClaims()))
	if !errors.Is(err, ErrSignature) {
		t.Fatalf("err = %v, want ErrSignature", err)
	}
}

// The classic JWT vulnerability: never trust the algorithm the token names.
// "none" would skip verification; an HMAC algorithm would let an attacker sign
// with the public key as the shared secret.
func TestAlgorithmConfusionIsRejected(t *testing.T) {
	s := newSigner(t, "kid-1")
	v := newTestVerifier(t, s)

	for _, alg := range []string{"none", "None", "HS256", "RS256", "ES384", ""} {
		_, err := v.Verify(context.Background(), s.mint(t, alg, validClaims()))
		if !errors.Is(err, ErrSignature) {
			t.Errorf("alg=%q: err = %v, want ErrSignature", alg, err)
		}
	}
}

// The subtle one. This token is genuinely signed by Google and genuinely issued
// by IAP — for a DIFFERENT application in the same organization. Without the
// audience check it would be accepted here.
func TestAssertionForAnotherApplicationIsRejected(t *testing.T) {
	s := newSigner(t, "kid-1")
	v := newTestVerifier(t, s)

	claims := validClaims()
	claims["aud"] = "/projects/123456789012/global/backendServices/111111111"
	if _, err := v.Verify(context.Background(), s.mint(t, "ES256", claims)); !errors.Is(err, ErrAudience) {
		t.Fatalf("err = %v, want ErrAudience", err)
	}

	// A prefix of the real audience must not pass either — the comparison is
	// exact, so a neighbouring backend id sharing a prefix cannot slip through.
	claims["aud"] = testAudience[:len(testAudience)-1]
	if _, err := v.Verify(context.Background(), s.mint(t, "ES256", claims)); !errors.Is(err, ErrAudience) {
		t.Fatalf("prefix audience: err = %v, want ErrAudience", err)
	}
}

func TestWrongIssuerIsRejected(t *testing.T) {
	s := newSigner(t, "kid-1")
	v := newTestVerifier(t, s)

	claims := validClaims()
	claims["iss"] = "https://accounts.google.com"
	if _, err := v.Verify(context.Background(), s.mint(t, "ES256", claims)); !errors.Is(err, ErrIssuer) {
		t.Fatalf("err = %v, want ErrIssuer", err)
	}
}

func TestExpiredAssertionIsRejected(t *testing.T) {
	s := newSigner(t, "kid-1")
	v := newTestVerifier(t, s)

	claims := validClaims()
	claims["exp"] = time.Now().Add(-time.Hour).Unix()
	if _, err := v.Verify(context.Background(), s.mint(t, "ES256", claims)); !errors.Is(err, ErrExpired) {
		t.Fatalf("err = %v, want ErrExpired", err)
	}

	// A token with no expiry at all must not be treated as never expiring.
	delete(claims, "exp")
	if _, err := v.Verify(context.Background(), s.mint(t, "ES256", claims)); !errors.Is(err, ErrExpired) {
		t.Fatalf("missing exp: err = %v, want ErrExpired", err)
	}
}

// Starting without an audience would silently disable the only check that
// distinguishes this application from every other IAP application in the org.
func TestVerifierRequiresAnAudience(t *testing.T) {
	if _, err := NewVerifier("", nil); err == nil {
		t.Fatal("NewVerifier with an empty audience: want an error")
	}
}

// Google rotates signing keys. A kid absent from the cached set must trigger
// exactly one refetch and then succeed.
func TestKeyRotationIsPickedUp(t *testing.T) {
	oldSigner := newSigner(t, "kid-old")
	newSigner2 := newSigner(t, "kid-new")

	fetches := 0
	current := []*signer{oldSigner}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fetches++
		keys := make([]map[string]string, 0, len(current))
		for _, s := range current {
			x := make([]byte, 32)
			y := make([]byte, 32)
			s.key.X.FillBytes(x)
			s.key.Y.FillBytes(y)
			keys = append(keys, map[string]string{
				"kid": s.kid, "crv": "P-256",
				"x": base64.RawURLEncoding.EncodeToString(x),
				"y": base64.RawURLEncoding.EncodeToString(y),
			})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": keys})
	}))
	defer srv.Close()

	v, err := NewVerifier(testAudience, nil)
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}
	v.keysURL = srv.URL

	if _, err := v.Verify(context.Background(), oldSigner.mint(t, "ES256", validClaims())); err != nil {
		t.Fatalf("before rotation: %v", err)
	}
	if fetches != 1 {
		t.Errorf("fetches = %d, want 1", fetches)
	}

	// A cached, unexpired key must not cause another fetch.
	if _, err := v.Verify(context.Background(), oldSigner.mint(t, "ES256", validClaims())); err != nil {
		t.Fatalf("second verify: %v", err)
	}
	if fetches != 1 {
		t.Errorf("cached key caused a refetch: fetches = %d", fetches)
	}

	// Rotation: the new kid is unknown, so exactly one refetch happens.
	current = []*signer{oldSigner, newSigner2}
	if _, err := v.Verify(context.Background(), newSigner2.mint(t, "ES256", validClaims())); err != nil {
		t.Fatalf("after rotation: %v", err)
	}
	if fetches != 2 {
		t.Errorf("fetches = %d, want 2", fetches)
	}
}

func TestFromRequestReadsTheHeader(t *testing.T) {
	s := newSigner(t, "kid-1")
	v := newTestVerifier(t, s)

	req := httptest.NewRequest(http.MethodGet, "/api/whoami", nil)
	req.Header.Set(HeaderName, s.mint(t, "ES256", validClaims()))
	if _, err := v.FromRequest(req); err != nil {
		t.Fatalf("FromRequest: %v", err)
	}

	bare := httptest.NewRequest(http.MethodGet, "/api/whoami", nil)
	if _, err := v.FromRequest(bare); !errors.Is(err, ErrMissing) {
		t.Fatalf("no header: err = %v, want ErrMissing", err)
	}
}

func TestKeyFetchFailureIsAnError(t *testing.T) {
	s := newSigner(t, "kid-1")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	v, _ := NewVerifier(testAudience, nil)
	v.keysURL = srv.URL

	_, err := v.Verify(context.Background(), s.mint(t, "ES256", validClaims()))
	if err == nil {
		t.Fatal("want an error when the key endpoint fails")
	}
	if got := fmt.Sprint(err); got == "" {
		t.Error("error should describe the fetch failure")
	}
}
