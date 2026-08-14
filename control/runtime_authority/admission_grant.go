// Copyright 2026 Mindclade. All rights reserved.
package runtime_authority

import "time"

type AdmissionGrantClaims struct {
	GrantID             string
	TenantID            string
	PrincipalID         string
	AllowedDeployments  []string
	AllowedCapabilities []string
	Region              string
	MaximumConcurrency  uint32
	MaximumRequests     uint64
	MaximumInputUnits   uint64
	MaximumOutputUnits  uint64
	NotBefore           time.Time
	Expires             time.Time
	PolicyEpoch         uint64
	RevocationEpoch     uint64
}

func (c AdmissionGrantClaims) ValidateStatic() error {
	if err := validateCanonicalID(c.GrantID, "grant"); err != nil {
		return err
	}
	if err := validateCanonicalID(c.TenantID, "tenant"); err != nil {
		return err
	}
	if c.PrincipalID == "" {
		return invalid("principal_id_required", "principal id is required", nil)
	}
	if err := requireSortedUnique(c.AllowedDeployments, "allowed_deployments"); err != nil {
		return err
	}
	if err := requireSortedUnique(c.AllowedCapabilities, "allowed_capabilities"); err != nil {
		return err
	}
	if c.Region == "" || c.MaximumConcurrency == 0 || c.MaximumRequests == 0 {
		return invalid("grant_budget_invalid", "grant region, concurrency, and request budgets are required", nil)
	}
	if c.NotBefore.IsZero() || c.Expires.IsZero() || !c.Expires.After(c.NotBefore) {
		return invalid("grant_time_window_invalid", "grant time window is invalid", nil)
	}
	if c.PolicyEpoch == 0 || c.RevocationEpoch == 0 {
		return invalid("grant_policy_epoch_required", "policy and revocation epochs must be non-zero", nil)
	}
	return nil
}
func (c AdmissionGrantClaims) CanonicalBytes() ([]byte, error) {
	if err := c.ValidateStatic(); err != nil {
		return nil, err
	}
	e := newCanonicalEncoder("admission-grant-claims")
	e.text("grant_id", c.GrantID)
	e.text("tenant_id", c.TenantID)
	e.text("principal_id", c.PrincipalID)
	e.stringSet("allowed_deployments", c.AllowedDeployments)
	e.stringSet("allowed_capabilities", c.AllowedCapabilities)
	e.text("region", c.Region)
	e.u32("maximum_concurrency", c.MaximumConcurrency)
	e.u64("maximum_requests", c.MaximumRequests)
	e.u64("maximum_input_units", c.MaximumInputUnits)
	e.u64("maximum_output_units", c.MaximumOutputUnits)
	notBefore, err := unixMillis(c.NotBefore, "not_before")
	if err != nil {
		return nil, err
	}
	expires, err := unixMillis(c.Expires, "expires")
	if err != nil {
		return nil, err
	}
	e.u64("not_before_unix_millis", notBefore)
	e.u64("expires_unix_millis", expires)
	e.u64("policy_epoch", c.PolicyEpoch)
	e.u64("revocation_epoch", c.RevocationEpoch)
	return e.bytes()
}
