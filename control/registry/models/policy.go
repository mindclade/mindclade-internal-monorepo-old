// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package models

import "time"

// PublicationPolicy decides which descriptors may be published as servable
// catalog state. It is durable control-plane policy: the data plane never
// re-evaluates it, it only honours the lifecycle the descriptor carries.
type PublicationPolicy struct {
	// RequiredCapabilities must all be declared by the model.
	RequiredCapabilities []string
	// AllowedAccelerators, when non-empty, restricts the accelerator
	// capability a serving descriptor may declare.
	AllowedAccelerators []string
	// MinimumPolicyEpoch rejects descriptors sealed under a retired policy.
	MinimumPolicyEpoch uint64
	// MinimumValidity is the shortest remaining lifetime a serving descriptor
	// may have at publication time. Publishing a descriptor that expires
	// moments later would strand the data plane with no servable route.
	MinimumValidity time.Duration
}

// Evaluate applies the policy to a descriptor at time now.
func (p PublicationPolicy) Evaluate(d Descriptor, now time.Time) error {
	if err := d.Validate(); err != nil {
		return err
	}
	if d.PolicyEpoch < p.MinimumPolicyEpoch {
		return invalid("model_policy_epoch_retired", "model descriptor was sealed under a retired policy epoch", nil)
	}
	declared := make(map[string]struct{}, len(d.Capabilities))
	for _, capability := range d.Capabilities {
		declared[capability] = struct{}{}
	}
	for _, required := range p.RequiredCapabilities {
		if _, ok := declared[required]; !ok {
			return invalid("model_capability_missing", "model descriptor is missing mandatory capability: "+required, nil)
		}
	}
	if len(p.AllowedAccelerators) > 0 {
		permitted := false
		for _, accelerator := range p.AllowedAccelerators {
			if accelerator == d.AcceleratorCapability {
				permitted = true
				break
			}
		}
		if !permitted {
			return invalid("model_accelerator_not_permitted", "model descriptor declares an accelerator capability that policy does not permit", nil)
		}
	}
	if d.Lifecycle != LifecycleServing {
		return nil
	}
	if !d.Expires.After(now.Add(p.MinimumValidity)) {
		return invalid("model_validity_too_short", "serving model descriptor does not retain the minimum remaining validity", nil)
	}
	return nil
}

// ProductionPolicy is the policy applied to descriptors published to the online
// inference path.
func ProductionPolicy() PublicationPolicy {
	return PublicationPolicy{
		RequiredCapabilities: []string{"structure"},
		MinimumPolicyEpoch:   1,
		MinimumValidity:      time.Hour,
	}
}
