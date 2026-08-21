// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package admission

import (
	"slices"
	"strconv"
	"strings"
	"time"

	"go.mindclade.dev/libs/go/identifiers"
	"go.mindclade.dev/libs/go/resourceversion"
)

// GatewayRoute is an exact provider/model binding. Wildcards are deliberately unsupported.
type GatewayRoute struct {
	Endpoint string `json:"endpoint"`
	Provider string `json:"provider"`
	Model    string `json:"model"`
}

func (route GatewayRoute) Validate() error {
	if err := validateName(route.Endpoint, "endpoint"); err != nil {
		return err
	}
	if err := validateName(route.Provider, "provider"); err != nil {
		return err
	}
	return validateName(route.Model, "model")
}

// Entitlement grants one subject exact routes and an upper bound per reservation.
type Entitlement struct {
	ID             identifiers.ID          `json:"id"`
	Subject        string                  `json:"subject"`
	Workspace      string                  `json:"workspace"`
	PolicyEpoch    uint64                  `json:"policy_epoch"`
	Routes         []GatewayRoute          `json:"routes"`
	MaximumRequest Quota                   `json:"maximum_request"`
	NotBefore      time.Time               `json:"not_before"`
	ExpiresAt      time.Time               `json:"expires_at"`
	Version        resourceversion.Version `json:"resource_version"`
}

func (e Entitlement) Validate() error {
	if err := e.validateBody(); err != nil {
		return err
	}
	if err := e.Version.Validate(); err != nil {
		return invalid("entitlement_version_invalid", "entitlement version is invalid", err)
	}
	if !e.Version.Digest().Equal(entitlementDigest(e)) {
		return invalid("entitlement_version_digest_mismatch", "entitlement version does not seal its content", nil)
	}
	return nil
}

func (e Entitlement) validateBody() error {
	if err := validateID(e.ID, "entitlement", "entitlement_id"); err != nil {
		return err
	}
	if err := validateName(e.Subject, "subject"); err != nil {
		return err
	}
	if err := validateName(e.Workspace, "workspace"); err != nil {
		return err
	}
	if e.PolicyEpoch == 0 {
		return invalid("policy_epoch_invalid", "policy epoch is required", nil)
	}
	if len(e.Routes) == 0 || len(e.Routes) > 128 {
		return invalid("entitlement_routes_invalid", "entitlement routes are outside bounds", nil)
	}
	seen := make(map[GatewayRoute]struct{}, len(e.Routes))
	for _, route := range e.Routes {
		if err := route.Validate(); err != nil {
			return err
		}
		if _, exists := seen[route]; exists {
			return invalid("entitlement_route_duplicate", "entitlement contains a duplicate route", nil)
		}
		seen[route] = struct{}{}
	}
	if err := e.MaximumRequest.Validate(true); err != nil {
		return err
	}
	if e.NotBefore.IsZero() || !e.ExpiresAt.After(e.NotBefore) {
		return invalid("entitlement_window_invalid", "entitlement window is invalid", nil)
	}
	return nil
}

// Seal canonicalizes route order and binds the policy content to generation.
func (e Entitlement) Seal(generation uint64) (Entitlement, error) {
	if err := e.validateBody(); err != nil {
		return Entitlement{}, err
	}
	e.Routes = append([]GatewayRoute(nil), e.Routes...)
	e.MaximumRequest = e.MaximumRequest.Clone()
	slices.SortFunc(e.Routes, compareGatewayRoute)
	version, err := resourceversion.New(generation, entitlementDigest(e))
	if err != nil {
		return Entitlement{}, invalid("entitlement_version_invalid", "entitlement version is invalid", err)
	}
	e.Version = version
	return e, nil
}

func entitlementDigest(e Entitlement) identifiers.Digest {
	routes := append([]GatewayRoute(nil), e.Routes...)
	slices.SortFunc(routes, compareGatewayRoute)
	values := []string{
		"gateway-entitlement-v1", e.ID.String(), e.Subject, e.Workspace,
		strconv.FormatUint(e.PolicyEpoch, 10), e.MaximumRequest.canonical(),
		e.NotBefore.UTC().Format(time.RFC3339Nano), e.ExpiresAt.UTC().Format(time.RFC3339Nano),
	}
	for _, route := range routes {
		values = append(values, route.Endpoint, route.Provider, route.Model)
	}
	return identifiers.SHA256String(canonicalJoin(values...))
}

func compareGatewayRoute(left, right GatewayRoute) int {
	if left.Endpoint != right.Endpoint {
		return strings.Compare(left.Endpoint, right.Endpoint)
	}
	if left.Provider != right.Provider {
		return strings.Compare(left.Provider, right.Provider)
	}
	return strings.Compare(left.Model, right.Model)
}

func (e Entitlement) ActiveAt(now time.Time) bool {
	return !now.Before(e.NotBefore) && now.Before(e.ExpiresAt)
}

func (e Entitlement) Allows(route GatewayRoute) bool {
	for _, allowed := range e.Routes {
		if allowed == route {
			return true
		}
	}
	return false
}

func (e Entitlement) clone() Entitlement {
	e.Routes = append([]GatewayRoute(nil), e.Routes...)
	e.MaximumRequest = e.MaximumRequest.Clone()
	return e
}
