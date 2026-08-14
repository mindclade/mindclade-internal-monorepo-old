// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

package production

import "sort"

// Capability names one concrete production mechanism wired by a process.
//
// Capabilities are deliberately mechanical. They describe reusable runtime,
// provider, coordination, and transport mechanisms that can be validated at
// process assembly time. They never encode tenant, model, scheduler,
// ingestion, registry, or product policy.
type Capability string

const (
	// Process-wide foundations.
	CapabilityServiceLifecycle Capability = "service_lifecycle"
	CapabilityClock            Capability = "clock"
	CapabilityConfiguration    Capability = "configuration"
	CapabilityIdentifiers      Capability = "identifiers"
	CapabilityRequestMetadata  Capability = "request_metadata"
	CapabilityObservability    Capability = "observability"
	CapabilityRetry            Capability = "retry"
	CapabilityPagination       Capability = "pagination"
	CapabilityResourceVersion  Capability = "resource_version"
	CapabilitySigning          Capability = "signing"

	// Security and durable mutation mechanisms.
	CapabilityAuthentication Capability = "authentication"
	CapabilityAuthorization  Capability = "authorization"
	CapabilityAudit          Capability = "audit"
	CapabilityIdempotency    Capability = "idempotency"
	CapabilityDatabase       Capability = "database"
	CapabilityTransactions   Capability = "transactions"

	// Provider mechanisms. These are passive unless the concrete adapter owns a
	// lifecycle component that is registered separately by the composition root.
	CapabilityBlobStore    Capability = "blob_store"
	CapabilityCache        Capability = "cache"
	CapabilityLeaseStore   Capability = "lease_store"
	CapabilityKubernetes   Capability = "kubernetes"
	CapabilityMessaging    Capability = "messaging"
	CapabilityOutboundHTTP Capability = "outbound_http"
	CapabilityMigrations   Capability = "migrations"

	// Durable coordination contracts and active loops are intentionally
	// separate. A store does not become a fake lifecycle component merely to
	// satisfy a profile.
	CapabilityOutboxStore       Capability = "outbox_store"
	CapabilityOutboxDispatcher  Capability = "outbox_dispatcher"
	CapabilityInboxProcessor    Capability = "inbox_processor"
	CapabilityCursorStore       Capability = "cursor_store"
	CapabilityWorkQueueStore    Capability = "work_queue_store"
	CapabilityWorkQueueWorker   Capability = "work_queue_worker"
	CapabilityLeadership        Capability = "leadership"
	CapabilityProjector         Capability = "projector"
	CapabilityKubernetesManager Capability = "kubernetes_manager"

	// Network mechanisms. Connect is normally mounted into the HTTP component,
	// while HTTP and gRPC own listener lifecycles.
	CapabilityHTTP    Capability = "http"
	CapabilityConnect Capability = "connect"
	CapabilityGRPC    Capability = "grpc"
)

// Deprecated source aliases retained for callers migrating from the first
// production-profile draft. New code must use the explicit store/worker names.
const (
	CapabilityLease     = CapabilityLeaseStore
	CapabilityOutbox    = CapabilityOutboxStore
	CapabilityInbox     = CapabilityInboxProcessor
	CapabilityCursor    = CapabilityCursorStore
	CapabilityWorkQueue = CapabilityWorkQueueStore
)

var allCapabilities = map[Capability]struct{}{
	CapabilityServiceLifecycle:  {},
	CapabilityClock:             {},
	CapabilityConfiguration:     {},
	CapabilityIdentifiers:       {},
	CapabilityRequestMetadata:   {},
	CapabilityObservability:     {},
	CapabilityRetry:             {},
	CapabilityPagination:        {},
	CapabilityResourceVersion:   {},
	CapabilitySigning:           {},
	CapabilityAuthentication:    {},
	CapabilityAuthorization:     {},
	CapabilityAudit:             {},
	CapabilityIdempotency:       {},
	CapabilityDatabase:          {},
	CapabilityTransactions:      {},
	CapabilityBlobStore:         {},
	CapabilityCache:             {},
	CapabilityLeaseStore:        {},
	CapabilityKubernetes:        {},
	CapabilityMessaging:         {},
	CapabilityOutboundHTTP:      {},
	CapabilityMigrations:        {},
	CapabilityOutboxStore:       {},
	CapabilityOutboxDispatcher:  {},
	CapabilityInboxProcessor:    {},
	CapabilityCursorStore:       {},
	CapabilityWorkQueueStore:    {},
	CapabilityWorkQueueWorker:   {},
	CapabilityLeadership:        {},
	CapabilityProjector:         {},
	CapabilityKubernetesManager: {},
	CapabilityHTTP:              {},
	CapabilityConnect:           {},
	CapabilityGRPC:              {},
}

func (capability Capability) String() string { return string(capability) }

func (capability Capability) Valid() bool {
	_, ok := allCapabilities[capability]
	return ok
}

// Active reports whether capability must be backed by a lifecycle-owning
// servicekit component rather than merely declared after construction.
func (capability Capability) Active() bool {
	_, ok := StageFor(capability)
	return ok
}

func sortedCapabilities(values map[Capability]struct{}) []Capability {
	result := make([]Capability, 0, len(values))
	for capability := range values {
		result = append(result, capability)
	}
	sort.Slice(result, func(left, right int) bool { return result[left] < result[right] })
	return result
}
