// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package admission

import (
	"time"

	"go.mindclade.dev/libs/go/identifiers"
	"go.mindclade.dev/libs/go/resourceversion"
)

// GatewayRoute is an exact provider/model binding. Wildcards are deliberately unsupported.
type GatewayRoute struct {
	Endpoint string
	Provider string
	Model    string
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
	ID             identifiers.ID
	Subject        string
	Workspace      string
	PolicyEpoch    uint64
	Routes         []GatewayRoute
	MaximumRequest Quota
	NotBefore      time.Time
	ExpiresAt      time.Time
	Version        resourceversion.Version
}

func (e Entitlement) Validate() error {
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
	if err := e.Version.Validate(); err != nil {
		return invalid("entitlement_version_invalid", "entitlement version is invalid", err)
	}
	return nil
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
