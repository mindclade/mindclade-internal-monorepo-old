// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package production

// Requirement is satisfied when at least one capability in AnyOf is wired.
// Single-capability requirements express mandatory mechanisms; multiple values
// express a deliberate alternative such as HTTP or gRPC transport.
type Requirement struct {
	Name  string
	AnyOf []Capability
}

func require(capability Capability) Requirement {
	return Requirement{Name: capability.String(), AnyOf: []Capability{capability}}
}

func requireAny(name string, capabilities ...Capability) Requirement {
	return Requirement{Name: name, AnyOf: append([]Capability(nil), capabilities...)}
}

func baseRequirements() []Requirement {
	return []Requirement{
		require(CapabilityServiceLifecycle),
		require(CapabilityClock),
		require(CapabilityConfiguration),
		require(CapabilityIdentifiers),
		require(CapabilityRequestMetadata),
		require(CapabilityObservability),
		require(CapabilityRetry),
	}
}

func appendRequirements(values ...Requirement) []Requirement {
	result := baseRequirements()
	return append(result, values...)
}

var profiles = map[Role][]Requirement{
	RoleAPI: appendRequirements(
		require(CapabilityDatabase), require(CapabilityTransactions),
		require(CapabilityAuthentication), require(CapabilityAuthorization),
		require(CapabilityAudit), require(CapabilityIdempotency), require(CapabilityPagination),
		require(CapabilityResourceVersion), require(CapabilitySigning), require(CapabilityOutboxStore),
		requireAny("network_transport", CapabilityHTTP, CapabilityGRPC),
	),
	RoleScheduler: appendRequirements(
		require(CapabilityDatabase), require(CapabilityTransactions), require(CapabilityAudit),
		require(CapabilityIdempotency), require(CapabilityLeaseStore), require(CapabilityKubernetes),
		require(CapabilityWorkQueueStore), require(CapabilityLeadership), require(CapabilityMessaging),
		require(CapabilityResourceVersion), require(CapabilitySigning), require(CapabilityOutboxStore),
	),
	RoleController: appendRequirements(
		require(CapabilityDatabase), require(CapabilityTransactions), require(CapabilityAudit),
		require(CapabilityIdempotency), require(CapabilityLeaseStore), require(CapabilityKubernetes),
		require(CapabilityKubernetesManager), require(CapabilityWorkQueueStore),
		require(CapabilityLeadership), require(CapabilityMessaging), require(CapabilityResourceVersion), require(CapabilityOutboxStore),
	),
	RoleOperator: appendRequirements(
		require(CapabilityDatabase), require(CapabilityTransactions), require(CapabilityAudit),
		require(CapabilityIdempotency), require(CapabilityLeaseStore), require(CapabilityKubernetes),
		require(CapabilityKubernetesManager), require(CapabilityWorkQueueStore),
		require(CapabilityLeadership), require(CapabilityMessaging), require(CapabilityResourceVersion), require(CapabilityOutboxStore),
	),
	RoleIngestionCoordinator: appendRequirements(
		require(CapabilityDatabase), require(CapabilityTransactions), require(CapabilityAudit),
		require(CapabilityIdempotency), require(CapabilityBlobStore), require(CapabilityCache),
		require(CapabilityLeaseStore), require(CapabilityKubernetes), require(CapabilityWorkQueueStore),
		require(CapabilityCursorStore), require(CapabilityLeadership), require(CapabilityMessaging),
		require(CapabilityResourceVersion), require(CapabilityOutboxStore),
	),
	RoleEventProjector: appendRequirements(
		require(CapabilityDatabase), require(CapabilityTransactions), require(CapabilityIdempotency),
		require(CapabilityLeaseStore), require(CapabilityInboxProcessor), require(CapabilityCursorStore),
		require(CapabilityLeadership), require(CapabilityMessaging), require(CapabilityProjector),
	),
	RoleDispatcher: appendRequirements(
		require(CapabilityDatabase), require(CapabilityMessaging), require(CapabilityOutboxStore), require(CapabilityOutboxDispatcher),
	),
	RoleWebhookDispatcher: appendRequirements(
		require(CapabilityDatabase), require(CapabilityTransactions), require(CapabilityAudit),
		require(CapabilityIdempotency), require(CapabilityWorkQueueStore),
		require(CapabilityWorkQueueWorker), require(CapabilityMessaging), require(CapabilityOutboundHTTP),
		require(CapabilitySigning), require(CapabilityOutboxStore),
	),
	RoleRegistry: appendRequirements(
		require(CapabilityDatabase), require(CapabilityTransactions),
		require(CapabilityAuthentication), require(CapabilityAuthorization),
		require(CapabilityAudit), require(CapabilityIdempotency), require(CapabilityPagination),
		require(CapabilityResourceVersion), require(CapabilitySigning),
		require(CapabilityBlobStore), require(CapabilityCache), require(CapabilityOutboxStore),
		requireAny("network_transport", CapabilityHTTP, CapabilityGRPC),
	),
	RoleAdmin: appendRequirements(
		require(CapabilityDatabase), require(CapabilityTransactions),
		require(CapabilityAuthentication), require(CapabilityAuthorization),
		require(CapabilityAudit), require(CapabilityIdempotency), require(CapabilityPagination),
		require(CapabilityResourceVersion), require(CapabilitySigning), require(CapabilityOutboxStore),
		requireAny("network_transport", CapabilityHTTP, CapabilityGRPC),
	),
	RoleMaintenance: appendRequirements(
		require(CapabilityDatabase), require(CapabilityTransactions), require(CapabilityAudit),
		require(CapabilityLeaseStore), require(CapabilityLeadership),
		require(CapabilityWorkQueueStore), require(CapabilityWorkQueueWorker),
	),
}

// Requirements returns an isolated copy of the role's production profile.
func Requirements(role Role) []Requirement {
	requirements := profiles[role]
	result := make([]Requirement, len(requirements))
	for index, requirement := range requirements {
		result[index] = Requirement{Name: requirement.Name, AnyOf: append([]Capability(nil), requirement.AnyOf...)}
	}
	return result
}
