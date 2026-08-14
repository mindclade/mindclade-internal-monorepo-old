// Copyright 2026 Mindclade. All rights reserved.
package signing

import (
	"encoding/base64"
	"encoding/json"
	"strings"
)

const MaximumSignatureBytes = 1024

type Signature struct {
	Algorithm Algorithm `json:"algorithm"`
	KeyID     KeyID     `json:"key_id"`
	Value     []byte    `json:"value"`
}

func NewSignature(algorithm Algorithm, keyID KeyID, value []byte) (Signature, error) {
	signature := Signature{Algorithm: algorithm, KeyID: keyID, Value: append([]byte(nil), value...)}
	if err := signature.Validate(); err != nil {
		return Signature{}, err
	}
	return signature, nil
}
func (signature Signature) Validate() error {
	if !signature.Algorithm.Valid() || !signature.KeyID.Valid() || len(signature.Value) == 0 || len(signature.Value) > MaximumSignatureBytes {
		return invalid(ErrInvalidSignature, "invalid detached signature", "invalid_signature", "signing.Signature.Validate", map[string]any{"key_id": signature.KeyID.String(), "algorithm": signature.Algorithm.String()})
	}
	return nil
}
func (signature Signature) Clone() Signature {
	signature.Value = append([]byte(nil), signature.Value...)
	return signature
}
func (signature Signature) MarshalText() ([]byte, error) {
	if err := signature.Validate(); err != nil {
		return nil, err
	}
	key := base64.RawURLEncoding.EncodeToString([]byte(signature.KeyID))
	value := base64.RawURLEncoding.EncodeToString(signature.Value)
	return []byte(signature.Algorithm.String() + "." + key + "." + value), nil
}
func (signature *Signature) UnmarshalText(value []byte) error {
	if signature == nil {
		return invalid(ErrInvalidSignature, "invalid detached signature", "nil_signature_destination", "signing.Signature.UnmarshalText", nil)
	}
	parts := strings.Split(string(value), ".")
	if len(parts) != 3 {
		return invalid(ErrInvalidSignature, "invalid detached signature", "invalid_signature_encoding", "signing.Signature.UnmarshalText", nil)
	}
	algorithm, err := ParseAlgorithm(parts[0])
	if err != nil {
		return err
	}
	keyBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return invalid(ErrInvalidSignature, "invalid detached signature", "invalid_signature_key_encoding", "signing.Signature.UnmarshalText", nil)
	}
	keyID, err := ParseKeyID(string(keyBytes))
	if err != nil {
		return err
	}
	signatureBytes, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return invalid(ErrInvalidSignature, "invalid detached signature", "invalid_signature_value_encoding", "signing.Signature.UnmarshalText", nil)
	}
	parsed, err := NewSignature(algorithm, keyID, signatureBytes)
	if err != nil {
		return err
	}
	*signature = parsed
	return nil
}
func (signature Signature) MarshalJSON() ([]byte, error) {
	if err := signature.Validate(); err != nil {
		return nil, err
	}
	type alias Signature
	return json.Marshal(alias(signature))
}
