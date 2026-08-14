// Copyright 2026 Mindclade. All rights reserved.
package signing

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
)

type HMACSigner struct {
	keyID KeyID
	key   []byte
}

func NewHMACSigner(keyID KeyID, key []byte) (*HMACSigner, error) {
	verification := VerificationKey{ID: keyID, Algorithm: AlgorithmHMACSHA256, HMACKey: key}
	if err := verification.Validate(); err != nil {
		return nil, err
	}
	return &HMACSigner{keyID: keyID, key: append([]byte(nil), key...)}, nil
}
func (signer *HMACSigner) Sign(ctx context.Context, payload []byte) (Signature, error) {
	if err := checkContext(ctx, "signing.HMACSigner.Sign"); err != nil {
		return Signature{}, err
	}
	if signer == nil || len(signer.key) < MinimumHMACKeySize {
		return Signature{}, invalid(ErrInvalidKey, "invalid HMAC signing key", "invalid_hmac_key", "signing.HMACSigner.Sign", nil)
	}
	mac := hmac.New(sha256.New, signer.key)
	_, _ = mac.Write(payload)
	return NewSignature(AlgorithmHMACSHA256, signer.keyID, mac.Sum(nil))
}
