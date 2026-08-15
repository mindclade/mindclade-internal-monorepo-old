// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

package session

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func testKey(t *testing.T, id string, fill byte) Key {
	t.Helper()
	material := make([]byte, 32)
	for i := range material {
		material[i] = fill
	}
	k, err := NewKey(id, material)
	if err != nil {
		t.Fatalf("NewKey(%q): %v", id, err)
	}
	return k
}

func testCodec(t *testing.T) *Codec {
	t.Helper()
	prev := testKey(t, "k1", 0x01)
	c, err := NewCodec(testKey(t, "k2", 0x02), &prev, 1)
	if err != nil {
		t.Fatalf("NewCodec: %v", err)
	}
	return c
}

func TestRoundTrip(t *testing.T) {
	c := testCodec(t)
	value, err := c.Seal("user-a", "sess-1")
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	claims, err := c.Open(value, "user-a")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if claims.Subject != "user-a" || claims.SessionID != "sess-1" {
		t.Errorf("claims = %+v", claims)
	}
	if got := claims.ExpiresAt.Sub(claims.IssuedAt); got != TTL {
		t.Errorf("lifetime = %v, want %v", got, TTL)
	}
}

// THE test. A valid cookie for principal A, replayed under principal B's IAP
// assertion, must be rejected. Without this the cookie is a bearer token and
// every other property is decoration.
func TestCrossPrincipalReplayIsRejected(t *testing.T) {
	c := testCodec(t)
	value, err := c.Seal("user-a", "sess-1")
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	if _, err := c.Open(value, "user-b"); !errors.Is(err, ErrSubject) {
		t.Fatalf("Open under a different subject: err = %v, want ErrSubject", err)
	}
}

// A cookie with no accompanying assertion is inert. This is the property that
// makes holding the session client-side safe at all.
func TestEmptyIAPSubjectIsRejected(t *testing.T) {
	c := testCodec(t)
	value, _ := c.Seal("user-a", "sess-1")
	if _, err := c.Open(value, ""); !errors.Is(err, ErrSubject) {
		t.Fatalf("err = %v, want ErrSubject", err)
	}
}

// Both keys are live: a cookie sealed before rotation still opens. Getting this
// wrong logs every user out at the rotation boundary, which presents as a total
// outage.
func TestPreviousKeyStillOpens(t *testing.T) {
	oldKey := testKey(t, "k1", 0x01)
	before, err := NewCodec(oldKey, nil, 1)
	if err != nil {
		t.Fatalf("NewCodec: %v", err)
	}
	value, err := before.Seal("user-a", "sess-1")
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}

	// Rotate: k2 becomes current, k1 is retained.
	after, err := NewCodec(testKey(t, "k2", 0x02), &oldKey, 1)
	if err != nil {
		t.Fatalf("NewCodec after rotation: %v", err)
	}
	if _, err := after.Open(value, "user-a"); err != nil {
		t.Fatalf("cookie sealed with the previous key: %v", err)
	}

	// Once k1 is retired, it stops opening — the other half of rotation.
	retired, err := NewCodec(testKey(t, "k2", 0x02), nil, 1)
	if err != nil {
		t.Fatalf("NewCodec: %v", err)
	}
	if _, err := retired.Open(value, "user-a"); !errors.Is(err, ErrUndecryptable) {
		t.Fatalf("after retiring the old key: err = %v, want ErrUndecryptable", err)
	}
}

// New cookies must use the newer key, or rotation never completes.
func TestSealUsesCurrentKey(t *testing.T) {
	c := testCodec(t)
	value, _ := c.Seal("user-a", "sess-1")
	if !strings.HasPrefix(value, "k2.") {
		t.Errorf("sealed with %q, want the current key k2", strings.SplitN(value, ".", 2)[0])
	}
}

func TestExpiry(t *testing.T) {
	c := testCodec(t)
	value, _ := c.Seal("user-a", "sess-1")

	c.now = func() time.Time { return time.Now().Add(TTL + time.Second) }
	if _, err := c.Open(value, "user-a"); !errors.Is(err, ErrExpired) {
		t.Fatalf("err = %v, want ErrExpired", err)
	}
}

// Bumping the authorization version invalidates every cached decision at once,
// without waiting out the TTL.
func TestAuthzVersionBumpInvalidates(t *testing.T) {
	c := testCodec(t)
	value, _ := c.Seal("user-a", "sess-1")

	prev := testKey(t, "k1", 0x01)
	bumped, err := NewCodec(testKey(t, "k2", 0x02), &prev, 2)
	if err != nil {
		t.Fatalf("NewCodec: %v", err)
	}
	if _, err := bumped.Open(value, "user-a"); !errors.Is(err, ErrExpired) {
		t.Fatalf("err = %v, want ErrExpired", err)
	}
}

// A ciphertext relabelled as the other key must not open. The key id is
// authenticated, not merely a routing hint.
func TestKeyIDIsAuthenticated(t *testing.T) {
	c := testCodec(t)
	value, _ := c.Seal("user-a", "sess-1")
	tampered := "k1" + value[2:]
	if _, err := c.Open(tampered, "user-a"); !errors.Is(err, ErrUndecryptable) {
		t.Fatalf("relabelled ciphertext: err = %v, want ErrUndecryptable", err)
	}
}

func TestTamperedCiphertextIsRejected(t *testing.T) {
	c := testCodec(t)
	value, _ := c.Seal("user-a", "sess-1")

	b := []byte(value)
	b[len(b)-1] ^= 0x01
	if _, err := c.Open(string(b), "user-a"); !errors.Is(err, ErrUndecryptable) {
		t.Fatalf("err = %v, want ErrUndecryptable", err)
	}
}

func TestMalformedInputs(t *testing.T) {
	c := testCodec(t)
	for _, value := range []string{
		"", "nodots", "one.two", ".a.b", "k2..", "k2.!!!.abc", "k2.YWJj.abc",
	} {
		if _, err := c.Open(value, "user-a"); err == nil {
			t.Errorf("Open(%q) succeeded; want an error", value)
		}
	}
}

func TestRejectsBadConfiguration(t *testing.T) {
	if _, err := NewKey("k", make([]byte, 16)); err == nil {
		t.Error("NewKey with a short key: want an error")
	}
	if _, err := NewCodec(Key{}, nil, 1); !errors.Is(err, ErrNoKeys) {
		t.Error("NewCodec with no current key: want ErrNoKeys")
	}
	dup := testKey(t, "k1", 0x01)
	if _, err := NewCodec(testKey(t, "k1", 0x02), &dup, 1); err == nil {
		t.Error("NewCodec with two keys sharing an id: want an error")
	}
	c := testCodec(t)
	if _, err := c.Seal("", "sess"); err == nil {
		t.Error("Seal with an empty subject: want an error")
	}
}

// Every cookie must be distinct even for identical claims, or a repeated
// ciphertext leaks that two sessions are the same.
func TestNonceIsFresh(t *testing.T) {
	c := testCodec(t)
	seen := make(map[string]struct{}, 100)
	for i := 0; i < 100; i++ {
		v, err := c.Seal("user-a", "sess-1")
		if err != nil {
			t.Fatalf("Seal: %v", err)
		}
		if _, dup := seen[v]; dup {
			t.Fatal("identical cookie produced twice")
		}
		seen[v] = struct{}{}
	}
}
