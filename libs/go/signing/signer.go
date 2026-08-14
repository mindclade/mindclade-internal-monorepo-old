// Copyright 2026 Mindclade. All rights reserved.
package signing

import "context"

type Signer interface {
	Sign(context.Context, []byte) (Signature, error)
}
type Verifier interface {
	Verify(context.Context, []byte, Signature) error
}
type SignerFunc func(context.Context, []byte) (Signature, error)

func (function SignerFunc) Sign(ctx context.Context, payload []byte) (Signature, error) {
	return function(ctx, payload)
}

type VerifierFunc func(context.Context, []byte, Signature) error

func (function VerifierFunc) Verify(ctx context.Context, payload []byte, signature Signature) error {
	return function(ctx, payload, signature)
}
