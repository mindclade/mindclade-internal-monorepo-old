// Copyright 2026 Mindclade. All rights reserved.
// Confidential and proprietary.

package audit

import (
	"encoding/json"
	"errors"
	"strings"

	"mindclade.internal/libs/go/auth"
	"mindclade.internal/libs/go/faults"
	"mindclade.internal/libs/go/identifiers"
)

// Actor is the identity snapshot stored in an audit event.
type Actor struct {
	kind           auth.PrincipalKind
	principalID    identifiers.ID
	subject        string
	issuer         string
	organizationID identifiers.ID
	tenantID       identifiers.ID
}

// ActorFromPrincipal snapshots a verified authentication principal.
func ActorFromPrincipal(principal auth.Principal) (Actor, error) {
	if err := principal.Validate(); err != nil {
		return Actor{}, invalidActor("invalid_audit_principal", "audit.ActorFromPrincipal", err)
	}
	actor := Actor{
		kind:           principal.Kind(),
		principalID:    principal.ID(),
		subject:        principal.Subject(),
		issuer:         principal.Issuer(),
		organizationID: principal.OrganizationID(),
		tenantID:       principal.TenantID(),
	}
	return actor, actor.Validate()
}

// NewSystemActor constructs a first-party system actor.
func NewSystemActor(subject string) (Actor, error) {
	principal, err := auth.NewPrincipal(
		auth.PrincipalKindSystem,
		strings.TrimSpace(subject),
		auth.WithIssuer("mindclade"),
	)
	if err != nil {
		return Actor{}, err
	}
	return ActorFromPrincipal(principal)
}

func (actor Actor) Kind() auth.PrincipalKind       { return actor.kind }
func (actor Actor) PrincipalID() identifiers.ID    { return actor.principalID }
func (actor Actor) Subject() string                { return actor.subject }
func (actor Actor) Issuer() string                 { return actor.issuer }
func (actor Actor) OrganizationID() identifiers.ID { return actor.organizationID }
func (actor Actor) TenantID() identifiers.ID       { return actor.tenantID }

func (actor Actor) Validate() error {
	options := []auth.PrincipalOption{auth.WithIssuer(actor.issuer)}
	if !actor.principalID.IsZero() {
		options = append(options, auth.WithPrincipalID(actor.principalID))
	}
	if !actor.organizationID.IsZero() {
		options = append(options, auth.WithOrganizationID(actor.organizationID))
	}
	if !actor.tenantID.IsZero() {
		options = append(options, auth.WithTenantID(actor.tenantID))
	}
	if _, err := auth.NewPrincipal(actor.kind, actor.subject, options...); err != nil {
		return invalidActor("invalid_audit_actor", "audit.Actor.Validate", err)
	}
	return nil
}

type actorJSON struct {
	Kind           auth.PrincipalKind `json:"kind"`
	PrincipalID    string             `json:"principal_id,omitempty"`
	Subject        string             `json:"subject"`
	Issuer         string             `json:"issuer"`
	OrganizationID string             `json:"organization_id,omitempty"`
	TenantID       string             `json:"tenant_id,omitempty"`
}

func (actor Actor) MarshalJSON() ([]byte, error) {
	if err := actor.Validate(); err != nil {
		return nil, err
	}
	return json.Marshal(actorJSON{
		Kind: actor.kind, PrincipalID: actor.principalID.String(), Subject: actor.subject,
		Issuer: actor.issuer, OrganizationID: actor.organizationID.String(), TenantID: actor.tenantID.String(),
	})
}

func (actor *Actor) UnmarshalJSON(value []byte) error {
	if actor == nil {
		return invalidActor("nil_audit_actor", "audit.Actor.UnmarshalJSON", nil)
	}
	var wire actorJSON
	if err := json.Unmarshal(value, &wire); err != nil {
		return invalidActor("malformed_audit_actor", "audit.Actor.UnmarshalJSON", err)
	}
	parsed := Actor{kind: wire.Kind, subject: wire.Subject, issuer: wire.Issuer}
	var err error
	if wire.PrincipalID != "" {
		parsed.principalID, err = identifiers.ParseID(wire.PrincipalID)
		if err != nil {
			return invalidActor("invalid_audit_principal_id", "audit.Actor.UnmarshalJSON", err)
		}
	}
	if wire.OrganizationID != "" {
		parsed.organizationID, err = identifiers.ParseID(wire.OrganizationID)
		if err != nil {
			return invalidActor("invalid_audit_organization_id", "audit.Actor.UnmarshalJSON", err)
		}
	}
	if wire.TenantID != "" {
		parsed.tenantID, err = identifiers.ParseID(wire.TenantID)
		if err != nil {
			return invalidActor("invalid_audit_tenant_id", "audit.Actor.UnmarshalJSON", err)
		}
	}
	if err := parsed.Validate(); err != nil {
		return err
	}
	*actor = parsed
	return nil
}

func invalidActor(reason, operation string, cause error) error {
	if cause == nil {
		cause = ErrInvalidActor
	} else {
		cause = errors.Join(ErrInvalidActor, cause)
	}
	return faults.Wrap(
		cause,
		faults.CodeInvalidArgument,
		"invalid audit actor",
		faults.WithReason(reason),
		faults.WithOperation(operation),
		faults.WithRetryPolicy(faults.NoRetry()),
	)
}
