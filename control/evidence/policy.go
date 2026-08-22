// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

package evidence

import (
	"sort"
	"time"

	"go.mindclade.dev/libs/go/identifiers"
)

type Control struct {
	ID               string        `json:"id"`
	Owner            string        `json:"owner"`
	MaximumAge       time.Duration `json:"maximum_age"`
	ExceptionAllowed bool          `json:"exception_allowed"`
}

type Policy struct {
	ID         string             `json:"id"`
	Version    string             `json:"version"`
	Digest     identifiers.Digest `json:"digest"`
	Epoch      uint64             `json:"epoch"`
	ValidUntil time.Time          `json:"valid_until"`
	Controls   []Control          `json:"controls"`
}

func (policy Policy) CanonicalBytes() ([]byte, error) {
	if err := policy.validate(false); err != nil {
		return nil, err
	}
	encoder := newCanonicalEncoder("production-control-policy/v1")
	encoder.text("id", policy.ID)
	encoder.text("version", policy.Version)
	encoder.u64("epoch", policy.Epoch)
	encoder.timestamp("valid_until", policy.ValidUntil)
	controls := make([]string, 0, len(policy.Controls))
	for _, control := range policy.Controls {
		allowed := "false"
		if control.ExceptionAllowed {
			allowed = "true"
		}
		controls = append(controls, control.ID+"|"+control.Owner+"|"+control.MaximumAge.String()+"|"+allowed)
	}
	encoder.strings("controls", controls)
	return encoder.bytes()
}

func (policy *Policy) Seal() error {
	if policy == nil {
		return invalid("evidence_policy_nil", "evidence policy is required", nil)
	}
	policy.Controls = append([]Control(nil), policy.Controls...)
	sort.Slice(policy.Controls, func(left, right int) bool { return policy.Controls[left].ID < policy.Controls[right].ID })
	payload, err := policy.CanonicalBytes()
	if err != nil {
		return err
	}
	policy.Digest = identifiers.SHA256(payload)
	return nil
}

func (policy Policy) Validate() error { return policy.validate(true) }

func (policy Policy) validate(requireDigest bool) error {
	if !canonicalName.MatchString(policy.ID) || !canonicalName.MatchString(policy.Version) || policy.Epoch == 0 || policy.ValidUntil.IsZero() || len(policy.Controls) == 0 || len(policy.Controls) > 128 {
		return invalid("evidence_policy_fields_invalid", "evidence policy fields are invalid", nil)
	}
	for index, control := range policy.Controls {
		if !canonicalName.MatchString(control.ID) || !canonicalName.MatchString(control.Owner) || control.MaximumAge <= 0 || control.MaximumAge > 366*24*time.Hour ||
			index > 0 && policy.Controls[index-1].ID >= control.ID {
			return invalid("evidence_policy_control_invalid", "evidence policy controls must be sorted, unique, and bounded", nil)
		}
	}
	if requireDigest {
		payload, err := policy.CanonicalBytes()
		if err != nil {
			return err
		}
		if !policy.Digest.Verify(payload) {
			return invalid("evidence_policy_digest_mismatch", "evidence policy digest does not match canonical content", nil)
		}
	}
	return nil
}
