// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

package production

import (
	"reflect"
	"testing"
)

func baseCapabilities() []Capability {
	return []Capability{
		CapabilityServiceLifecycle,
		CapabilityClock,
		CapabilityConfiguration,
		CapabilityIdentifiers,
		CapabilityRequestMetadata,
		CapabilityObservability,
		CapabilityRetry,
	}
}

func capabilities(values ...Capability) []Capability {
	result := append([]Capability(nil), baseCapabilities()...)
	return append(result, values...)
}

func TestAPIProfileAcceptsConnectMountedOnHTTP(t *testing.T) {
	manifest, err := NewManifest(
		"control-plane-api",
		RoleAPI,
		capabilities(
			CapabilityDatabase,
			CapabilityTransactions,
			CapabilityAuthentication,
			CapabilityAuthorization,
			CapabilityAudit,
			CapabilityIdempotency,
			CapabilityPagination,
			CapabilityResourceVersion,
			CapabilitySigning,
			CapabilityOutboxStore,
			CapabilityHTTP,
			CapabilityConnect,
		)...,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !manifest.Has(CapabilityHTTP) || !manifest.Has(CapabilityConnect) || manifest.Role() != RoleAPI {
		t.Fatalf("unexpected manifest: %#v", manifest)
	}
	if _, err := manifest.NewAssembly(); err != nil {
		t.Fatal(err)
	}
}

func TestSchedulerProfileRejectsMissingCoordination(t *testing.T) {
	_, err := NewManifest(
		"scheduler",
		RoleScheduler,
		capabilities(
			CapabilityDatabase,
			CapabilityTransactions,
			CapabilityAudit,
			CapabilityKubernetes,
		)...,
	)
	if err == nil {
		t.Fatal("incomplete scheduler profile accepted")
	}
}

func TestRequirementsReturnsIsolatedCopy(t *testing.T) {
	first := Requirements(RoleEventProjector)
	second := Requirements(RoleEventProjector)
	first[0].AnyOf[0] = CapabilityHTTP
	if reflect.DeepEqual(first, second) {
		t.Fatal("requirements share mutable backing storage")
	}
}

func TestCapabilitiesAreCanonicalAndSorted(t *testing.T) {
	manifest, err := NewManifest(
		"dispatcher",
		RoleDispatcher,
		capabilities(
			CapabilityDatabase,
			CapabilityMessaging,
			CapabilityOutboxStore,
			CapabilityOutboxDispatcher,
		)...,
	)
	if err != nil {
		t.Fatal(err)
	}
	want := []Capability{
		CapabilityClock,
		CapabilityConfiguration,
		CapabilityDatabase,
		CapabilityIdentifiers,
		CapabilityMessaging,
		CapabilityObservability,
		CapabilityOutboxDispatcher,
		CapabilityOutboxStore,
		CapabilityRequestMetadata,
		CapabilityRetry,
		CapabilityServiceLifecycle,
	}
	if got := manifest.Capabilities(); !reflect.DeepEqual(got, want) {
		t.Fatalf("capabilities=%v want=%v", got, want)
	}
}

func TestWebhookDispatcherHasDistinctProfile(t *testing.T) {
	manifest, err := NewManifest(
		"webhook-dispatcher",
		RoleWebhookDispatcher,
		capabilities(
			CapabilityDatabase,
			CapabilityTransactions,
			CapabilityAudit,
			CapabilityIdempotency,
			CapabilityWorkQueueStore,
			CapabilityWorkQueueWorker,
			CapabilityMessaging,
			CapabilityOutboundHTTP,
			CapabilitySigning,
			CapabilityOutboxStore,
		)...,
	)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Role() != RoleWebhookDispatcher {
		t.Fatalf("role=%s", manifest.Role())
	}
}
