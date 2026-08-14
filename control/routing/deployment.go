// Copyright 2026 Mindclade. All rights reserved.
package routing

import (
	"mindclade.internal/control/runtime_authority"
	"sort"
)

type Deployment = runtime_authority.DeploymentRoute

func CanonicalDeployments(in []Deployment) ([]Deployment, error) {
	out := append([]Deployment(nil), in...)
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
