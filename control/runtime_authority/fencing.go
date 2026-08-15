// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package runtime_authority

type FencingFloor interface{ MinimumFencingToken(resource string) uint64 }
type StaticFencingFloor map[string]uint64

func (f StaticFencingFloor) MinimumFencingToken(resource string) uint64 { return f[resource] }
func ValidateFencing(resource string, token uint64, floor FencingFloor) error {
	if token == 0 {
		return invalid("fencing_token_required", "fencing token is required", nil)
	}
	if floor != nil && token < floor.MinimumFencingToken(resource) {
		return invalid("stale_fencing_token", "fencing token is stale", nil)
	}
	return nil
}
