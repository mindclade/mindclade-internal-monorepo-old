// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package foundation

import (
	"slices"
	"testing"

	mcclock "go.mindclade.dev/libs/go/clock"
	"go.mindclade.dev/libs/go/servicekit/production"
)

func TestCoreReportsOnlyWiredCapabilities(t *testing.T) {
	capabilities := Core{Clock: mcclock.RealClock{}}.Capabilities()
	if !slices.Contains(capabilities, production.CapabilityClock) {
		t.Fatalf("capabilities=%v", capabilities)
	}
	for _, absent := range []production.Capability{
		production.CapabilityConfiguration,
		production.CapabilityIdentifiers,
		production.CapabilityObservability,
		production.CapabilityRetry,
	} {
		if slices.Contains(capabilities, absent) {
			t.Fatalf("unwired capability %q reported: %v", absent, capabilities)
		}
	}
}

// A contract field holding a typed nil is not a wired capability. Reporting it
// would let a process start and fail on first use instead of at startup.
func TestTypedNilIsNotAWiredCapability(t *testing.T) {
	var absent mcclock.Clock
	if capabilities := (Core{Clock: absent}).Capabilities(); len(capabilities) != 0 {
		t.Fatalf("capabilities=%v", capabilities)
	}
	if !IsNil(absent) {
		t.Fatal("typed nil interface reported as present")
	}
}

func TestCapabilitiesAreSortedDeduplicatedAndStable(t *testing.T) {
	declarations := []Declaration{
		{Capability: production.CapabilityRetry, Present: true},
		{Capability: production.CapabilityClock, Present: true},
		{Capability: production.CapabilityClock, Present: true},
		{Capability: production.CapabilityCache, Present: false},
	}
	first := Present(declarations)
	if !slices.IsSorted(first) || len(first) != 2 {
		t.Fatalf("present=%v", first)
	}
	if second := Present(declarations); !slices.Equal(first, second) {
		t.Fatalf("unstable: %v vs %v", first, second)
	}
}

func TestServiceOptionsAreOmittedWhenNothingIsWired(t *testing.T) {
	if options := (Core{}).ServiceOptions(); len(options) != 0 {
		t.Fatalf("options=%d", len(options))
	}
}

func TestRegisterRejectsANilBuilder(t *testing.T) {
	if err := Register(nil, nil); err == nil {
		t.Fatal("nil builder accepted")
	}
}
