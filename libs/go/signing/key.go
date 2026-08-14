// Copyright 2026 Mindclade. All rights reserved.
package signing

import (
	"crypto/ed25519"
	"strings"
	"time"
)

const (
	MaximumKeyIDLength = 128
	MinimumHMACKeySize = 32
)

type KeyID string

func ParseKeyID(value string) (KeyID, error) {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > MaximumKeyIDLength {
		return "", invalid(ErrInvalidKeyID, "invalid signing key id", "invalid_signing_key_id", "signing.ParseKeyID", map[string]any{"key_id": value})
	}
	for index, character := range value {
		if character >= 'a' && character <= 'z' || character >= '0' && character <= '9' {
			continue
		}
		if index > 0 && (character == '-' || character == '_' || character == '.' || character == '/') {
			continue
		}
		return "", invalid(ErrInvalidKeyID, "invalid signing key id", "invalid_signing_key_id", "signing.ParseKeyID", map[string]any{"key_id": value})
	}
	return KeyID(value), nil
}
func MustParseKeyID(value string) KeyID {
	keyID, err := ParseKeyID(value)
	if err != nil {
		panic(err)
	}
	return keyID
}
func (keyID KeyID) String() string { return string(keyID) }
func (keyID KeyID) Valid() bool {
	parsed, err := ParseKeyID(string(keyID))
	return err == nil && parsed == keyID
}

// VerificationKey is one immutable key-version entry in a KeySet. Exactly one
// key material field must match Algorithm.
type VerificationKey struct {
	ID               KeyID
	Algorithm        Algorithm
	HMACKey          []byte
	Ed25519PublicKey ed25519.PublicKey
	NotBefore        time.Time
	NotAfter         time.Time
	Disabled         bool
}

func (key VerificationKey) Validate() error {
	if !key.ID.Valid() || !key.Algorithm.Valid() {
		return invalid(ErrInvalidKey, "invalid signing verification key", "invalid_verification_key", "signing.VerificationKey.Validate", map[string]any{"key_id": key.ID.String(), "algorithm": key.Algorithm.String()})
	}
	switch key.Algorithm {
	case AlgorithmHMACSHA256:
		if len(key.HMACKey) < MinimumHMACKeySize || len(key.Ed25519PublicKey) != 0 {
			return invalid(ErrInvalidKey, "invalid HMAC verification key", "invalid_hmac_key", "signing.VerificationKey.Validate", map[string]any{"key_id": key.ID.String()})
		}
	case AlgorithmEd25519:
		if len(key.Ed25519PublicKey) != ed25519.PublicKeySize || len(key.HMACKey) != 0 {
			return invalid(ErrInvalidKey, "invalid Ed25519 verification key", "invalid_ed25519_key", "signing.VerificationKey.Validate", map[string]any{"key_id": key.ID.String()})
		}
	}
	if !key.NotBefore.IsZero() && !key.NotAfter.IsZero() && !key.NotAfter.After(key.NotBefore) {
		return invalid(ErrInvalidKey, "invalid signing-key validity interval", "invalid_key_validity", "signing.VerificationKey.Validate", map[string]any{"key_id": key.ID.String()})
	}
	return nil
}
func (key VerificationKey) active(at time.Time) bool {
	at = at.UTC()
	return !key.Disabled && (key.NotBefore.IsZero() || !at.Before(key.NotBefore.UTC())) && (key.NotAfter.IsZero() || at.Before(key.NotAfter.UTC()))
}
func cloneVerificationKey(key VerificationKey) VerificationKey {
	key.HMACKey = append([]byte(nil), key.HMACKey...)
	key.Ed25519PublicKey = append(ed25519.PublicKey(nil), key.Ed25519PublicKey...)
	key.NotBefore = key.NotBefore.Round(0).UTC()
	key.NotAfter = key.NotAfter.Round(0).UTC()
	return key
}
