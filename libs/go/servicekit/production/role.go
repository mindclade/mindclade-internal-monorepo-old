// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package production

// Role is a stable process category, not a service-specific domain model.
type Role string

const (
	RoleAPI                  Role = "api"
	RoleScheduler            Role = "scheduler"
	RoleController           Role = "controller"
	RoleOperator             Role = "operator"
	RoleIngestionCoordinator Role = "ingestion_coordinator"
	RoleEventProjector       Role = "event_projector"
	RoleDispatcher           Role = "dispatcher"
	RoleWebhookDispatcher    Role = "webhook_dispatcher"
	RoleRegistry             Role = "registry"
	RoleAdmin                Role = "admin"
	RoleMaintenance          Role = "maintenance"
)

func (role Role) String() string { return string(role) }

func (role Role) Valid() bool {
	switch role {
	case RoleAPI, RoleScheduler, RoleController, RoleOperator, RoleIngestionCoordinator,
		RoleEventProjector, RoleDispatcher, RoleWebhookDispatcher, RoleRegistry,
		RoleAdmin, RoleMaintenance:
		return true
	default:
		return false
	}
}
