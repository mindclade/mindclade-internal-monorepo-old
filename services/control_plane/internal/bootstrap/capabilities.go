// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package bootstrap

import (
	"sort"

	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/servicekit/production"
)

// Capabilities returns the deduplicated union of what aggregates provide. The
// production Builder performs the authoritative role validation; this exists
// for diagnostics and for tests that assert a composition before building it.
func Capabilities(aggregates ...Aggregate) []production.Capability {
	seen := make(map[production.Capability]struct{})
	for _, aggregate := range aggregates {
		if aggregate == nil {
			continue
		}
		for _, capability := range aggregate.Capabilities() {
			seen[capability] = struct{}{}
		}
	}
	result := make([]production.Capability, 0, len(seen))
	for capability := range seen {
		result = append(result, capability)
	}
	sort.Slice(result, func(left, right int) bool { return result[left] < result[right] })
	return result
}

// RequireCapabilities reports which of required is not provided by aggregates.
func RequireCapabilities(aggregates []Aggregate, required ...production.Capability) error {
	available := make(map[production.Capability]struct{})
	for _, capability := range Capabilities(aggregates...) {
		available[capability] = struct{}{}
	}
	missing := make([]string, 0)
	for _, capability := range required {
		if capability == "" {
			continue
		}
		if _, ok := available[capability]; !ok {
			missing = append(missing, capability.String())
		}
	}
	if len(missing) == 0 {
		return nil
	}
	sort.Strings(missing)
	return faults.New(
		faults.CodeFailedPrecondition,
		"control-plane process dependencies are incomplete",
		faults.WithReason("missing_process_capabilities"),
		faults.WithOperation("controlplane.bootstrap.RequireCapabilities"),
		faults.WithField("missing_capabilities", missing),
		faults.WithRetryPolicy(faults.NoRetry()),
	)
}
