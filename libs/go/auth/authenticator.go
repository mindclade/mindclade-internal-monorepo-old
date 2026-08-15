// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package auth

import (
	"context"
	"errors"
	"time"

	"mindclade.internal/libs/go/faults"
)

type Authenticator interface {
	Authenticate(context.Context, Credential) (Principal, error)
}

type AuthenticatorFunc func(context.Context, Credential) (Principal, error)

func (function AuthenticatorFunc) Authenticate(ctx context.Context, credential Credential) (Principal, error) {
	return function(ctx, credential)
}

// Authenticate validates inputs and the returned principal. Provider errors
// retain their original fault classification.
func Authenticate(ctx context.Context, authenticator Authenticator, credential Credential) (Principal, error) {
	return AuthenticateAt(ctx, authenticator, credential, time.Now().UTC(), 0)
}

func AuthenticateAt(ctx context.Context, authenticator Authenticator, credential Credential, now time.Time, skew time.Duration) (Principal, error) {
	if ctx == nil {
		return Principal{}, newFault(ErrNilContext, faults.CodeInvalidArgument, "nil authentication context", "nil_context", "auth.Authenticate", nil)
	}
	if nilInterface(authenticator) {
		return Principal{}, newFault(ErrNilAuthenticator, faults.CodeFailedPrecondition, "authentication is not configured", "authenticator_missing", "auth.Authenticate", nil)
	}
	if err := credential.Validate(); err != nil {
		return Principal{}, err
	}
	principal, err := authenticator.Authenticate(ctx, credential)
	if err != nil {
		return Principal{}, preserveFault(err, faults.PublicMessageOf(err), "auth.Authenticate")
	}
	if err := principal.Validate(); err != nil {
		return Principal{}, err
	}
	if principal.Expired(now, skew) {
		return Principal{}, newFault(errors.Join(ErrUnauthenticated, ErrInvalidPrincipal), faults.CodeUnauthenticated, "authentication has expired", "principal_expired", "auth.Authenticate", nil)
	}
	return principal, nil
}

// ChainAuthenticator tries providers in order. Only ErrUnsupportedCredential
// advances to the next provider; all other failures stop the chain.
type ChainAuthenticator struct{ providers []Authenticator }

func NewChainAuthenticator(providers ...Authenticator) (*ChainAuthenticator, error) {
	filtered := make([]Authenticator, 0, len(providers))
	for _, provider := range providers {
		if !nilInterface(provider) {
			filtered = append(filtered, provider)
		}
	}
	if len(filtered) == 0 {
		return nil, newFault(ErrNilAuthenticator, faults.CodeFailedPrecondition, "authentication is not configured", "authenticator_missing", "auth.NewChainAuthenticator", nil)
	}
	return &ChainAuthenticator{providers: filtered}, nil
}

func (chain *ChainAuthenticator) Authenticate(ctx context.Context, credential Credential) (Principal, error) {
	if chain == nil || len(chain.providers) == 0 {
		return Principal{}, newFault(ErrNilAuthenticator, faults.CodeFailedPrecondition, "authentication is not configured", "authenticator_missing", "auth.ChainAuthenticator.Authenticate", nil)
	}
	var unsupported []error
	for _, provider := range chain.providers {
		principal, err := provider.Authenticate(ctx, credential)
		if err == nil {
			return principal, nil
		}
		if errors.Is(err, ErrUnsupportedCredential) {
			unsupported = append(unsupported, err)
			continue
		}
		return Principal{}, err
	}
	return Principal{}, newFault(errors.Join(append([]error{ErrUnsupportedCredential}, unsupported...)...), faults.CodeUnauthenticated, "credential scheme is not supported", "unsupported_credential", "auth.ChainAuthenticator.Authenticate", faults.Fields{"scheme": credential.Scheme().String()})
}
