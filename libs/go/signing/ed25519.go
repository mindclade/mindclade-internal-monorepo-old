// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package signing

import (
	"context"
	"crypto/ed25519"
)

type Ed25519Signer struct {
	keyID KeyID
	key   ed25519.PrivateKey
}

func NewEd25519Signer(keyID KeyID, key ed25519.PrivateKey) (*Ed25519Signer, error) {
	if !keyID.Valid() || len(key) != ed25519.PrivateKeySize {
		return nil, invalid(ErrInvalidKey, "invalid Ed25519 signing key", "invalid_ed25519_private_key", "signing.NewEd25519Signer", map[string]any{"key_id": keyID.String()})
	}
	return &Ed25519Signer{keyID: keyID, key: append(ed25519.PrivateKey(nil), key...)}, nil
}
func (signer *Ed25519Signer) Sign(ctx context.Context, payload []byte) (Signature, error) {
	if err := checkContext(ctx, "signing.Ed25519Signer.Sign"); err != nil {
		return Signature{}, err
	}
	if signer == nil || len(signer.key) != ed25519.PrivateKeySize {
		return Signature{}, invalid(ErrInvalidKey, "invalid Ed25519 signing key", "invalid_ed25519_private_key", "signing.Ed25519Signer.Sign", nil)
	}
	return NewSignature(AlgorithmEd25519, signer.keyID, ed25519.Sign(signer.key, payload))
}
