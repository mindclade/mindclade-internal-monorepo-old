// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package routing

import "mindclade.internal/libs/go/identifiers"

type Policy struct {
	PolicyEpoch           uint64
	RevocationEpoch       uint64
	MinimumRuntimeVersion string
	PolicyDigest          identifiers.Digest
}

func (p Policy) Validate() error {
	if p.PolicyEpoch == 0 || p.RevocationEpoch == 0 || p.MinimumRuntimeVersion == "" || !p.PolicyDigest.Valid() {
		return invalid("route_policy_invalid", "route policy epochs, runtime version, and digest are required", nil)
	}
	return nil
}
