// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

package bootstrap

import (
	"sort"
	"strings"

	"mindclade.internal/libs/go/faults"
	"mindclade.internal/libs/go/servicekit/production"
)

// Role identifies one deployable Go process. Roles share a production
// foundation but never share domain policy, repositories, or executable state.
type Role string

const (
	RoleAPI                 Role = "api"
	RoleAdmin               Role = "admin"
	RoleController          Role = "controller"
	RoleOperator            Role = "operator"
	RoleScheduler           Role = "scheduler"
	RoleIngestionController Role = "ingestion-controller"
	RoleEventProjector      Role = "event-projector"
	RoleEventDispatcher     Role = "event-dispatcher"
	RoleWebhookDispatcher   Role = "webhook-dispatcher"
	RoleRegistry            Role = "registry"
	RoleMaintenance         Role = "maintenance"
)

func (role Role) String() string { return string(role) }

// Profile binds a repository process name to the single canonical
// servicekit/production role profile. Capability requirements are owned only by
// libs/go/servicekit/production and are not copied into service packages.
type Profile struct {
	Role           Role
	Name           string
	ProductionRole production.Role
}

func (profile Profile) Validate() error {
	if profile.Role == "" || strings.TrimSpace(profile.Name) == "" || strings.TrimSpace(profile.Name) != profile.Name || !profile.ProductionRole.Valid() {
		return invalidProfile("invalid_profile_identity")
	}
	expected, err := productionRole(profile.Role)
	if err != nil {
		return err
	}
	if expected != profile.ProductionRole {
		return invalidProfile("profile_role_mismatch")
	}
	return nil
}

// Requirements returns an isolated copy of the canonical production
// requirements for the profile.
func (profile Profile) Requirements() []production.Requirement {
	return production.Requirements(profile.ProductionRole)
}

// ProfileFor returns the single supported profile for role.
func ProfileFor(role Role) (Profile, error) {
	productionRole, err := productionRole(role)
	if err != nil {
		return Profile{}, err
	}
	name := map[Role]string{
		RoleAPI:                 "control-plane-api",
		RoleAdmin:               "control-plane-admin",
		RoleController:          "control-plane-controller",
		RoleOperator:            "control-plane-operator",
		RoleScheduler:           "control-plane-scheduler",
		RoleIngestionController: "control-plane-ingestion-controller",
		RoleEventProjector:      "control-plane-event-projector",
		RoleEventDispatcher:     "control-plane-event-dispatcher",
		RoleWebhookDispatcher:   "control-plane-webhook-dispatcher",
		RoleRegistry:            "control-plane-registry",
		RoleMaintenance:         "control-plane-maintenance",
	}[role]
	profile := Profile{Role: role, Name: name, ProductionRole: productionRole}
	if err := profile.Validate(); err != nil {
		return Profile{}, err
	}
	return profile, nil
}

// Profiles returns all supported profiles in stable role order.
func Profiles() []Profile {
	roles := []Role{
		RoleAPI,
		RoleAdmin,
		RoleController,
		RoleOperator,
		RoleScheduler,
		RoleIngestionController,
		RoleEventProjector,
		RoleEventDispatcher,
		RoleWebhookDispatcher,
		RoleRegistry,
		RoleMaintenance,
	}
	profiles := make([]Profile, 0, len(roles))
	for _, role := range roles {
		profile, err := ProfileFor(role)
		if err == nil {
			profiles = append(profiles, profile)
		}
	}
	sort.SliceStable(profiles, func(left, right int) bool { return profiles[left].Role < profiles[right].Role })
	return profiles
}

func productionRole(role Role) (production.Role, error) {
	switch role {
	case RoleAPI:
		return production.RoleAPI, nil
	case RoleAdmin:
		return production.RoleAdmin, nil
	case RoleController:
		return production.RoleController, nil
	case RoleOperator:
		return production.RoleOperator, nil
	case RoleScheduler:
		return production.RoleScheduler, nil
	case RoleIngestionController:
		return production.RoleIngestionCoordinator, nil
	case RoleEventProjector:
		return production.RoleEventProjector, nil
	case RoleEventDispatcher:
		return production.RoleDispatcher, nil
	case RoleWebhookDispatcher:
		return production.RoleWebhookDispatcher, nil
	case RoleRegistry:
		return production.RoleRegistry, nil
	case RoleMaintenance:
		return production.RoleMaintenance, nil
	default:
		return "", faults.New(
			faults.CodeInvalidArgument,
			"unsupported control-plane process role",
			faults.WithReason("unsupported_process_role"),
			faults.WithOperation("controlplane.bootstrap.productionRole"),
			faults.WithField("role", string(role)),
			faults.WithRetryPolicy(faults.NoRetry()),
		)
	}
}

func invalidProfile(reason string) error {
	return faults.New(
		faults.CodeInvalidArgument,
		"invalid control-plane process profile",
		faults.WithReason(reason),
		faults.WithOperation("controlplane.bootstrap.Profile.Validate"),
		faults.WithRetryPolicy(faults.NoRetry()),
	)
}
