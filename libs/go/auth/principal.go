// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package auth

import (
	"errors"
	"strings"
	"time"

	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/identifiers"
)

type PrincipalKind string

const (
	PrincipalKindUser     PrincipalKind = "user"
	PrincipalKindService  PrincipalKind = "service"
	PrincipalKindWorkload PrincipalKind = "workload"
	PrincipalKindAgent    PrincipalKind = "agent"
	PrincipalKindSystem   PrincipalKind = "system"
)

func ParsePrincipalKind(value string) (PrincipalKind, error) {
	kind := PrincipalKind(strings.ToLower(strings.TrimSpace(value)))
	if !kind.Valid() {
		return "", newFault(ErrInvalidPrincipal, faults.CodeInvalidArgument, "invalid principal kind", "invalid_principal_kind", "auth.ParsePrincipalKind", nil)
	}
	return kind, nil
}

func (kind PrincipalKind) String() string { return string(kind) }
func (kind PrincipalKind) Valid() bool {
	switch kind {
	case PrincipalKindUser, PrincipalKindService, PrincipalKindWorkload, PrincipalKindAgent, PrincipalKindSystem:
		return true
	default:
		return false
	}
}

// Principal is an immutable authenticated identity snapshot.
type Principal struct {
	kind            PrincipalKind
	id              identifiers.ID
	issuer          string
	subject         string
	organizationID  identifiers.ID
	tenantID        identifiers.ID
	permissions     PermissionSet
	attributes      map[string]string
	authenticatedAt time.Time
	expiresAt       time.Time
}

type PrincipalOption func(*Principal) error

func WithPrincipalID(identifier identifiers.ID) PrincipalOption {
	return func(principal *Principal) error { principal.id = identifier; return nil }
}
func WithIssuer(issuer string) PrincipalOption {
	return func(principal *Principal) error { principal.issuer = strings.TrimSpace(issuer); return nil }
}
func WithOrganizationID(identifier identifiers.ID) PrincipalOption {
	return func(principal *Principal) error { principal.organizationID = identifier; return nil }
}
func WithTenantID(identifier identifiers.ID) PrincipalOption {
	return func(principal *Principal) error { principal.tenantID = identifier; return nil }
}
func WithPermissions(permissions PermissionSet) PrincipalOption {
	return func(principal *Principal) error {
		principal.permissions = permissions.Merge(PermissionSet{})
		return nil
	}
}
func WithPrincipalAttributes(attributes map[string]string) PrincipalOption {
	captured := cloneAttributes(attributes)
	return func(principal *Principal) error {
		normalized, err := normalizePrincipalAttributes(captured)
		if err != nil {
			return err
		}
		principal.attributes = normalized
		return nil
	}
}
func WithAuthenticationTimes(authenticatedAt, expiresAt time.Time) PrincipalOption {
	return func(principal *Principal) error {
		principal.authenticatedAt = authenticatedAt.Round(0).UTC()
		principal.expiresAt = expiresAt.Round(0).UTC()
		return nil
	}
}

func NewPrincipal(kind PrincipalKind, subject string, options ...PrincipalOption) (Principal, error) {
	principal := Principal{kind: kind, subject: strings.TrimSpace(subject)}
	for _, option := range options {
		if option != nil {
			if err := option(&principal); err != nil {
				return Principal{}, err
			}
		}
	}
	principal.permissions = principal.permissions.Merge(PermissionSet{})
	principal.attributes = cloneAttributes(principal.attributes)
	if err := principal.Validate(); err != nil {
		return Principal{}, err
	}
	return principal, nil
}

func (principal Principal) Kind() PrincipalKind            { return principal.kind }
func (principal Principal) ID() identifiers.ID             { return principal.id }
func (principal Principal) Issuer() string                 { return principal.issuer }
func (principal Principal) Subject() string                { return principal.subject }
func (principal Principal) OrganizationID() identifiers.ID { return principal.organizationID }
func (principal Principal) TenantID() identifiers.ID       { return principal.tenantID }
func (principal Principal) Permissions() PermissionSet {
	return principal.permissions.Merge(PermissionSet{})
}
func (principal Principal) Attributes() map[string]string {
	return cloneAttributes(principal.attributes)
}
func (principal Principal) AuthenticatedAt() time.Time { return principal.authenticatedAt }
func (principal Principal) ExpiresAt() time.Time       { return principal.expiresAt }
func (principal Principal) IsZero() bool {
	return principal.kind == "" && principal.subject == "" && principal.issuer == ""
}

func (principal Principal) Validate() error {
	if !principal.kind.Valid() || !validIdentityText(principal.subject, 512) || !validIdentityText(principal.issuer, 512) {
		return newFault(
			ErrInvalidPrincipal,
			faults.CodeUnauthenticated,
			"invalid authenticated principal",
			"invalid_principal",
			"auth.Principal.Validate",
			faults.Fields{"principal_kind": principal.kind.String()},
		)
	}
	for _, identifier := range []identifiers.ID{principal.id, principal.organizationID, principal.tenantID} {
		if !identifier.IsZero() {
			if err := identifier.Validate(); err != nil {
				return newFault(errors.Join(ErrInvalidPrincipal, err), faults.CodeUnauthenticated, "invalid authenticated principal", "invalid_principal_identifier", "auth.Principal.Validate", nil)
			}
		}
	}
	if err := principal.permissions.Validate(); err != nil {
		return err
	}
	if _, err := normalizePrincipalAttributes(principal.attributes); err != nil {
		return err
	}
	if !principal.authenticatedAt.IsZero() && !principal.expiresAt.IsZero() && !principal.expiresAt.After(principal.authenticatedAt) {
		return newFault(ErrInvalidPrincipal, faults.CodeUnauthenticated, "invalid authenticated principal", "invalid_principal_lifetime", "auth.Principal.Validate", nil)
	}
	return nil
}

func (principal Principal) Expired(now time.Time, skew time.Duration) bool {
	if principal.expiresAt.IsZero() {
		return false
	}
	if skew < 0 {
		skew = 0
	}
	return !now.Before(principal.expiresAt.Add(skew))
}

func (principal Principal) Allows(permission Permission) bool {
	return principal.permissions.Allows(permission)
}

// Key returns a stable, non-PII key for the issuer/subject pair. The NUL
// separator is unambiguous because identity text rejects control characters.
func (principal Principal) Key() string {
	if principal.IsZero() {
		return ""
	}
	return identifiers.SHA256String(principal.issuer + "\x00" + principal.subject).String()
}
