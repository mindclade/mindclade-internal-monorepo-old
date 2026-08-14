// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

package bootstrap

import "sort"

// FoundationCoverage returns the union of packages expected to be consumed by
// the supported Go process roles. Repository qualification compares this list
// with the public package inventory in libs/go/LAYERS.md.
func FoundationCoverage() []string {
	set := make(map[string]struct{})
	for _, consumption := range ConsumptionMatrix() {
		for _, packagePath := range consumption.Packages {
			set[packagePath] = struct{}{}
		}
	}
	result := make([]string, 0, len(set))
	for packagePath := range set {
		result = append(result, packagePath)
	}
	sort.Strings(result)
	return result
}
