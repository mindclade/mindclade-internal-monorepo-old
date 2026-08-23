// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package routing

import (
	"go.mindclade.dev/control/runtime_authority"
	"sort"
)

type Deployment = runtime_authority.DeploymentRoute

func CanonicalDeployments(in []Deployment) ([]Deployment, error) {
	out := make([]Deployment, len(in))
	for i := range in {
		out[i] = in[i]
		// Deployment contains a slice, so copying the struct alone would let the
		// canonicalizer reorder caller-owned policy in place.
		out[i].Capabilities = append([]string(nil), in[i].Capabilities...)
	}
	for i := range out {
		sort.Strings(out[i].Capabilities)
		if err := out[i].Validate(); err != nil {
			return nil, err
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].DeploymentID < out[j].DeploymentID })
	for i := 1; i < len(out); i++ {
		if out[i-1].DeploymentID == out[i].DeploymentID {
			return nil, invalid("duplicate_deployment", "route policy contains duplicate deployment", nil)
		}
	}
	return out, nil
}
