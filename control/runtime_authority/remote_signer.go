// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package runtime_authority

import (
	"context"
	"fmt"

	"mindclade.internal/libs/go/signing"
)

// AsymmetricSignClient is the narrow provider seam used by the durable control
// plane for HSM/KMS-backed signing. Implementations must sign the exact MCCE1
// payload bytes supplied by Sign; hashing or canonicalization is not delegated
// to the provider adapter.
//
// Provider-specific clients live at the service/provider edge, not in libs/go
// or this domain package.
type AsymmetricSignClient interface {
	SignEd25519(context.Context, string, []byte) ([]byte, error)
}

// RemoteEd25519Signer adapts a managed asymmetric key version to the shared
// detached signing.Signer contract used by Issuer.
type RemoteEd25519Signer struct {
	KeyID      signing.KeyID
	KeyVersion string
	Client     AsymmetricSignClient
}

func (s RemoteEd25519Signer) Sign(ctx context.Context, payload []byte) (signing.Signature, error) {
	if ctx == nil {
		return signing.Signature{}, invalid("remote_signer_nil_context", "remote signer context is required", nil)
	}
	if !s.KeyID.Valid() || s.KeyVersion == "" || len(s.KeyVersion) > 1024 || s.Client == nil {
		return signing.Signature{}, invalid("remote_signer_unconfigured", "remote Ed25519 signer is not configured", nil)
	}
	signature, err := s.Client.SignEd25519(ctx, s.KeyVersion, append([]byte(nil), payload...))
	if err != nil {
		return signing.Signature{}, fmt.Errorf("remote Ed25519 sign %q: %w", s.KeyVersion, err)
	}
	if len(signature) != 64 {
		return signing.Signature{}, invalid("remote_signature_length_invalid", "remote Ed25519 signer returned an invalid signature length", nil)
	}
	return signing.NewSignature(signing.AlgorithmEd25519, s.KeyID, signature)
}
