// Copyright 2026 Mindclade. All rights reserved.
package runtime_authority

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"testing"

	"mindclade.internal/libs/go/signing"
)

type fakeAsymmetricSigner struct{ key ed25519.PrivateKey }

func (f fakeAsymmetricSigner) SignEd25519(_ context.Context, _ string, payload []byte) ([]byte, error) {
	return ed25519.Sign(f.key, payload), nil
}

func TestRemoteEd25519Signer(t *testing.T) {
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	keyID, err := signing.ParseKeyID("runtime-authority/v1")
	if err != nil {
		t.Fatal(err)
	}
	signer := RemoteEd25519Signer{
		KeyID:      keyID,
		KeyVersion: "providers/example/keys/runtime/versions/1",
		Client:     fakeAsymmetricSigner{key: privateKey},
	}
	signature, err := signer.Sign(context.Background(), []byte("claims"))
	if err != nil {
		t.Fatal(err)
	}
	if signature.Algorithm != signing.AlgorithmEd25519 || len(signature.Value) != ed25519.SignatureSize {
		t.Fatalf("unexpected signature: %#v", signature)
	}
}
