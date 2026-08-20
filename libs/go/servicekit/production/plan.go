// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package production

import (
	"strings"

	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/servicekit"
)

// validateRuntimePlan prevents a role from satisfying its capability manifest
// with stores and provider adapters while omitting the process loop or network
// boundary that gives the executable its purpose.
func validateRuntimePlan(service string, role Role, entries []servicekit.AssemblyEntry) error {
	counts := make(map[servicekit.Stage]int)
	for _, entry := range entries {
		counts[entry.Stage]++
	}

	var required servicekit.Stage
	switch role {
	case RoleAPI, RoleRegistry, RoleAdmin:
		required = servicekit.StageServing
	case RoleScheduler, RoleController, RoleOperator, RoleIngestionCoordinator,
		RoleEventProjector, RoleWebhookDispatcher, RoleMaintenance:
		required = servicekit.StageWork
	case RoleDispatcher:
		required = servicekit.StageCoordination
	default:
		return builderFault(
			"invalid_production_role",
			"invalid production role",
			"servicekit.production.validateRuntimePlan",
			service,
			role,
			nil,
		)
	}
	if counts[required] == 0 {
		return builderFault(
			"missing_role_runtime_stage",
			"production process has no component in its required runtime stage",
			"servicekit.production.validateRuntimePlan",
			service,
			role,
			faults.Fields{"required_stage": required.String()},
		)
	}
	if counts[servicekit.StageFoundation] == 0 {
		return builderFault(
			"missing_foundation_component",
			"production process has no foundation component",
			"servicekit.production.validateRuntimePlan",
			service,
			role,
			faults.Fields{"required_stage": servicekit.StageFoundation.String()},
		)
	}

	// Component names form part of diagnostics and evidence. Reject accidental
	// empty or whitespace-only names even though servicekit normally catches
	// them earlier, keeping the plan validator safe for independently decoded
	// manifests in qualification tooling.
	for _, entry := range entries {
		if strings.TrimSpace(entry.Component) == "" {
			return builderFault(
				"invalid_runtime_component",
				"production runtime contains an invalid component",
				"servicekit.production.validateRuntimePlan",
				service,
				role,
				faults.Fields{"stage": entry.Stage.String()},
			)
		}
	}
	return nil
}
