// Copyright 2026 Mindclade. All rights reserved.
// Confidential and proprietary.

package auth

import (
	"errors"
	"fmt"
	"strings"

	"mindclade.internal/libs/go/faults"
)

const MaximumCredentialBytes = 16 * 1024

type CredentialScheme string

const (
	CredentialSchemeBearer  CredentialScheme = "bearer"
	CredentialSchemeAPIKey  CredentialScheme = "api_key"
	CredentialSchemeBasic   CredentialScheme = "basic"
	CredentialSchemeMTLS    CredentialScheme = "mtls"
	CredentialSchemeSession CredentialScheme = "session"
)

func ParseCredentialScheme(value string) (CredentialScheme, error) {
	normalized := CredentialScheme(strings.ToLower(strings.TrimSpace(value)))
	if !normalized.Valid() {
		return "", newFault(
			errors.Join(ErrInvalidCredential, fmt.Errorf("invalid scheme %q", value)),
			faults.CodeInvalidArgument,
			"invalid credential scheme",
			"invalid_credential_scheme",
			"auth.ParseCredentialScheme",
			nil,
		)
	}
	return normalized, nil
}

func (scheme CredentialScheme) String() string { return string(scheme) }

func (scheme CredentialScheme) Valid() bool {
	value := string(scheme)
	if value == "" || len(value) > 32 {
		return false
	}
	for index := 0; index < len(value); index++ {
		character := value[index]
		if !(character >= 'a' && character <= 'z') && !(character >= '0' && character <= '9') && character != '_' && character != '-' {
			return false
		}
	}
	return true
}

// Credential contains opaque authentication evidence. Its String method never
// exposes the value, and Value returns a defensive copy.
type Credential struct {
	scheme     CredentialScheme
	value      []byte
	attributes map[string]string
}

type CredentialOption func(*Credential) error

func WithCredentialAttributes(attributes map[string]string) CredentialOption {
	captured := cloneAttributes(attributes)
	return func(credential *Credential) error {
		normalized, err := normalizeCredentialAttributes(captured)
		if err != nil {
			return err
		}
		credential.attributes = normalized
		return nil
	}
}

func NewCredential(scheme CredentialScheme, value []byte, options ...CredentialOption) (Credential, error) {
	credential := Credential{scheme: scheme, value: append([]byte(nil), value...)}
	for _, option := range options {
		if option != nil {
			if err := option(&credential); err != nil {
				return Credential{}, err
			}
		}
	}
	if err := credential.Validate(); err != nil {
		return Credential{}, err
	}
	return credential, nil
}

func Bearer(token string) (Credential, error) {
	return NewCredential(CredentialSchemeBearer, []byte(token))
}

func APIKey(value string) (Credential, error) {
	return NewCredential(CredentialSchemeAPIKey, []byte(value))
}

func (credential Credential) Scheme() CredentialScheme { return credential.scheme }
func (credential Credential) Value() []byte            { return append([]byte(nil), credential.value...) }
func (credential Credential) Attributes() map[string]string {
	return cloneAttributes(credential.attributes)
}
func (credential Credential) IsZero() bool {
	return credential.scheme == "" && len(credential.value) == 0
}
func (credential Credential) String() string {
	if credential.scheme == "" {
		return "credential(<empty>)"
	}
	return "credential(" + credential.scheme.String() + ":[REDACTED])"
}

// GoString prevents %#v formatting from exposing the opaque credential bytes.
func (credential Credential) GoString() string { return credential.String() }

// Format redacts the credential for every fmt formatting verb.
func (credential Credential) Format(state fmt.State, _ rune) {
	_, _ = state.Write([]byte(credential.String()))
}

func (credential Credential) Validate() error {
	if !credential.scheme.Valid() || len(credential.value) == 0 || len(credential.value) > MaximumCredentialBytes {
		return newFault(
			ErrInvalidCredential,
			faults.CodeUnauthenticated,
			"invalid authentication credential",
			"invalid_credential",
			"auth.Credential.Validate",
			faults.Fields{"scheme": credential.scheme.String(), "credential_bytes": len(credential.value)},
		)
	}
	if _, err := normalizeCredentialAttributes(credential.attributes); err != nil {
		return err
	}
	return nil
}
