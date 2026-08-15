// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package runtime_authority

import (
	"go.mindclade.dev/libs/go/identifiers"
	"sort"
	"time"
)

type DeploymentRoute struct {
	DeploymentID       string
	ModelBundleDigest  identifiers.Digest
	EngineBundleDigest identifiers.Digest
	Endpoint           string
	Region             string
	Weight             uint32
	Capabilities       []string
	LeaseExpires       time.Time
	SafetyPolicyDigest identifiers.Digest
}

func (r DeploymentRoute) Validate() error {
	if err := validateCanonicalID(r.DeploymentID, "deployment"); err != nil {
		return err
	}
	if !r.ModelBundleDigest.Valid() || !r.EngineBundleDigest.Valid() {
		return invalid("route_bundle_digest_required", "route model and engine bundle digests are required", nil)
	}
	if r.Endpoint == "" || r.Region == "" || r.Weight == 0 || r.LeaseExpires.IsZero() {
		return invalid("route_fields_required", "route endpoint, region, weight, and lease expiry are required", nil)
	}
	return requireSortedUnique(r.Capabilities, "route_capabilities")
}
func (r DeploymentRoute) canonicalBytes() ([]byte, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}
	e := newCanonicalEncoder("deployment-route")
	e.text("deployment_id", r.DeploymentID)
	e.text("model_bundle_digest", r.ModelBundleDigest.String())
	e.text("engine_bundle_digest", r.EngineBundleDigest.String())
	e.text("endpoint", r.Endpoint)
	e.text("region", r.Region)
	e.u32("weight", r.Weight)
	e.stringSet("capabilities", r.Capabilities)
	leaseExpires, err := unixMillis(r.LeaseExpires, "lease_expires")
	if err != nil {
		return nil, err
	}
	e.u64("lease_expires_unix_millis", leaseExpires)
	e.text("safety_policy_digest", r.SafetyPolicyDigest.String())
	return e.bytes()
}

type RouteSnapshotClaims struct {
	SnapshotID            string
	SnapshotDigest        identifiers.Digest
	Version               uint64
	PolicyEpoch           uint64
	RevocationEpoch       uint64
	Created               time.Time
	Expires               time.Time
	Routes                []DeploymentRoute
	MinimumRuntimeVersion string
}

func (c RouteSnapshotClaims) ValidateStatic() error {
	if err := validateCanonicalID(c.SnapshotID, "routesnap"); err != nil {
		return err
	}
	if c.Version == 0 || c.PolicyEpoch == 0 || c.RevocationEpoch == 0 {
		return invalid("route_snapshot_version_required", "route snapshot versions and epochs must be non-zero", nil)
	}
	if c.Created.IsZero() || c.Expires.IsZero() || !c.Expires.After(c.Created) {
		return invalid("route_snapshot_time_window_invalid", "route snapshot time window is invalid", nil)
	}
	if c.MinimumRuntimeVersion == "" {
		return invalid("minimum_runtime_version_required", "minimum runtime version is required", nil)
	}
	last := ""
	for _, r := range c.Routes {
		if err := r.Validate(); err != nil {
			return err
		}
		if last != "" && last >= r.DeploymentID {
			return invalid("routes_not_canonical", "routes must be strictly sorted by deployment id", nil)
		}
		last = r.DeploymentID
	}
	return nil
}
func (c RouteSnapshotClaims) bytesWithoutDigest() ([]byte, error) {
	clone := c
	clone.SnapshotDigest = identifiers.Digest{}
	return clone.canonical(false)
}
func (c RouteSnapshotClaims) canonical(includeDigest bool) ([]byte, error) {
	if err := c.ValidateStatic(); err != nil {
		return nil, err
	}
	e := newCanonicalEncoder("route-snapshot-claims")
	e.text("snapshot_id", c.SnapshotID)
	if includeDigest {
		e.text("snapshot_digest", c.SnapshotDigest.String())
	} else {
		e.text("snapshot_digest", "")
	}
	e.u64("version", c.Version)
	e.u64("policy_epoch", c.PolicyEpoch)
	e.u64("revocation_epoch", c.RevocationEpoch)
	created, err := unixMillis(c.Created, "created")
	if err != nil {
		return nil, err
	}
	expires, err := unixMillis(c.Expires, "expires")
	if err != nil {
		return nil, err
	}
	e.u64("created_unix_millis", created)
	e.u64("expires_unix_millis", expires)
	var nested []byte
	for _, r := range c.Routes {
		rb, err := r.canonicalBytes()
		if err != nil {
			return nil, err
		}
		enc := newCanonicalEncoder("route-list-entry")
		enc.nested("route", rb)
		entry, err := enc.bytes()
		if err != nil {
			return nil, err
		}
		nested = append(nested, entry...)
	}
	e.nested("routes", nested)
	e.text("minimum_runtime_version", c.MinimumRuntimeVersion)
	return e.bytes()
}
func (c RouteSnapshotClaims) CanonicalBytes() ([]byte, error) { return c.canonical(true) }
func (c *RouteSnapshotClaims) SealDigest() error {
	if c == nil {
		return invalid("nil_route_snapshot", "route snapshot is required", nil)
	}
	sort.Slice(c.Routes, func(i, j int) bool { return c.Routes[i].DeploymentID < c.Routes[j].DeploymentID })
	for i := range c.Routes {
		sort.Strings(c.Routes[i].Capabilities)
	}
	b, err := c.bytesWithoutDigest()
	if err != nil {
		return err
	}
	c.SnapshotDigest = identifiers.SHA256(b)
	return nil
}
func (c RouteSnapshotClaims) VerifyDigest() error {
	if !c.SnapshotDigest.Valid() {
		return invalid("route_snapshot_digest_required", "route snapshot digest is required", nil)
	}
	b, err := c.bytesWithoutDigest()
	if err != nil {
		return err
	}
	if !c.SnapshotDigest.Equal(identifiers.SHA256(b)) {
		return invalid("route_snapshot_digest_mismatch", "route snapshot digest does not match claims", nil)
	}
	return nil
}
