// Copyright 2026 Mindclade. All rights reserved.
package signing

import (
	"context"
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/sha256"
	"sync"
	"time"

	"mindclade.internal/libs/go/clock"
)

type KeySet struct {
	mu    sync.RWMutex
	keys  map[KeyID]VerificationKey
	clock clock.Clock
}

func NewKeySet(value clock.Clock, keys ...VerificationKey) (*KeySet, error) {
	if value == nil {
		value = clock.RealClock{}
	}
	set := &KeySet{keys: make(map[KeyID]VerificationKey), clock: value}
	for _, key := range keys {
		if err := set.Add(key); err != nil {
			return nil, err
		}
	}
	return set, nil
}
func (set *KeySet) Add(key VerificationKey) error {
	if set == nil {
		return invalid(ErrInvalidKey, "invalid signing key set", "nil_keyset", "signing.KeySet.Add", nil)
	}
	if err := key.Validate(); err != nil {
		return err
	}
	set.mu.Lock()
	defer set.mu.Unlock()
	if _, exists := set.keys[key.ID]; exists {
		return invalid(ErrInvalidKey, "signing key already exists", "duplicate_signing_key", "signing.KeySet.Add", map[string]any{"key_id": key.ID.String()})
	}
	set.keys[key.ID] = cloneVerificationKey(key)
	return nil
}
func (set *KeySet) Replace(key VerificationKey) error {
	if set == nil {
		return invalid(ErrInvalidKey, "invalid signing key set", "nil_keyset", "signing.KeySet.Replace", nil)
	}
	if err := key.Validate(); err != nil {
		return err
	}
	set.mu.Lock()
	set.keys[key.ID] = cloneVerificationKey(key)
	set.mu.Unlock()
	return nil
}
func (set *KeySet) Remove(keyID KeyID) bool {
	if set == nil {
		return false
	}
	set.mu.Lock()
	_, exists := set.keys[keyID]
	delete(set.keys, keyID)
	set.mu.Unlock()
	return exists
}
func (set *KeySet) Snapshot() []VerificationKey {
	if set == nil {
		return nil
	}
	set.mu.RLock()
	result := make([]VerificationKey, 0, len(set.keys))
	for _, key := range set.keys {
		result = append(result, cloneVerificationKey(key))
	}
	set.mu.RUnlock()
	return result
}
func (set *KeySet) Verify(ctx context.Context, payload []byte, signature Signature) error {
	if err := checkContext(ctx, "signing.KeySet.Verify"); err != nil {
		return err
	}
	if err := signature.Validate(); err != nil {
		return err
	}
	if set == nil {
		return verifyFault(ErrUnknownKey, "verification_keyset_unavailable", "signing.KeySet.Verify", signature.KeyID)
	}
	set.mu.RLock()
	key, exists := set.keys[signature.KeyID]
	set.mu.RUnlock()
	if !exists {
		return verifyFault(ErrUnknownKey, "unknown_signature_key", "signing.KeySet.Verify", signature.KeyID)
	}
	now := time.Now().UTC()
	if set.clock != nil {
		now = set.clock.Now().UTC()
	}
	if !key.active(now) {
		return verifyFault(ErrInactiveKey, "inactive_signature_key", "signing.KeySet.Verify", signature.KeyID)
	}
	if key.Algorithm != signature.Algorithm {
		return verifyFault(ErrVerification, "signature_algorithm_mismatch", "signing.KeySet.Verify", signature.KeyID)
	}
	valid := false
	switch key.Algorithm {
	case AlgorithmHMACSHA256:
		mac := hmac.New(sha256.New, key.HMACKey)
		_, _ = mac.Write(payload)
		valid = hmac.Equal(mac.Sum(nil), signature.Value)
	case AlgorithmEd25519:
		valid = ed25519.Verify(key.Ed25519PublicKey, payload, signature.Value)
	}
	if !valid {
		return verifyFault(ErrVerification, "signature_value_mismatch", "signing.KeySet.Verify", signature.KeyID)
	}
	return nil
}
