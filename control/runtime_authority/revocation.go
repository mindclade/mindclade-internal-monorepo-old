// Copyright 2026 Mindclade. All rights reserved.
package runtime_authority

import "time"

type RevocationSnapshotClaims struct {
	Epoch                uint64
	Created              time.Time
	Expires              time.Time
	RevokedGrantIDs      []string
	RevokedTicketIDs     []string
	RevokedDeploymentIDs []string
	RevokedBundleDigests []string
}

func (c RevocationSnapshotClaims) ValidateStatic() error {
	if c.Epoch == 0 {
		return invalid("revocation_epoch_required", "revocation epoch must be non-zero", nil)
	}
	if c.Created.IsZero() || c.Expires.IsZero() || !c.Expires.After(c.Created) {
		return invalid("revocation_time_window_invalid", "revocation snapshot time window is invalid", nil)
	}
	for name, values := range map[string][]string{"revoked_grant_ids": c.RevokedGrantIDs, "revoked_ticket_ids": c.RevokedTicketIDs, "revoked_deployment_ids": c.RevokedDeploymentIDs, "revoked_bundle_digests": c.RevokedBundleDigests} {
		if err := requireSortedUnique(values, name); err != nil {
			return err
		}
	}
	return nil
}
func (c RevocationSnapshotClaims) CanonicalBytes() ([]byte, error) {
	if err := c.ValidateStatic(); err != nil {
		return nil, err
	}
	e := newCanonicalEncoder("revocation-snapshot-claims")
	e.u64("epoch", c.Epoch)
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
	e.stringSet("revoked_grant_ids", c.RevokedGrantIDs)
	e.stringSet("revoked_ticket_ids", c.RevokedTicketIDs)
	e.stringSet("revoked_deployment_ids", c.RevokedDeploymentIDs)
	e.stringSet("revoked_bundle_digests", c.RevokedBundleDigests)
	return e.bytes()
}

type Revocations interface {
	MinimumEpoch() uint64
	GrantRevoked(string) bool
	TicketRevoked(string) bool
	DeploymentRevoked(string) bool
	BundleRevoked(string) bool
}
type SnapshotRevocations struct{ Claims RevocationSnapshotClaims }

func (s SnapshotRevocations) MinimumEpoch() uint64 { return s.Claims.Epoch }
func containsSorted(v []string, x string) bool {
	for _, y := range v {
		if y == x {
			return true
		}
		if y > x {
			return false
		}
	}
	return false
}
func (s SnapshotRevocations) GrantRevoked(v string) bool {
	return containsSorted(s.Claims.RevokedGrantIDs, v)
}
func (s SnapshotRevocations) TicketRevoked(v string) bool {
	return containsSorted(s.Claims.RevokedTicketIDs, v)
}
func (s SnapshotRevocations) DeploymentRevoked(v string) bool {
	return containsSorted(s.Claims.RevokedDeploymentIDs, v)
}
func (s SnapshotRevocations) BundleRevoked(v string) bool {
	return containsSorted(s.Claims.RevokedBundleDigests, v)
}
