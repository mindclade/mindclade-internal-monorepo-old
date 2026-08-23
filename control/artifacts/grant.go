// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package artifacts

import (
	"time"

	"go.mindclade.dev/libs/go/faults"
)

// MaximumGrantTTL bounds how far ahead a grant may be issued.
//
// A grant names digests and namespaces a tenant may reach; it is not a session.
// The byte plane re-checks the bound on every operation, so a long-lived grant
// is authority that survives every revocation this package can express -- there
// is no revocation list here, so expiry is the only way a grant ever stops
// being honoured.
const MaximumGrantTTL = time.Hour

type AccessGrant struct {
	TenantID           string
	ReadableDigests    []string
	WritableNamespaces []string
	MaximumReadBytes   uint64
	MaximumWriteBytes  uint64
	// IssuedAt and ExpiresAt bound the grant in time.
	//
	// This type previously had neither, so a grant was authority that never
	// expired by construction: nothing in the struct could express "no longer
	// valid", and Validate could not have rejected an ancient one. The Rust
	// byte plane it authorizes has always carried an expiry and re-checks it
	// per operation (services/artifact_proxy/src/grants.rs, require_active), so
	// the control-plane type was the weaker half of a contract the two sides
	// were supposed to share.
	IssuedAt  time.Time
	ExpiresAt time.Time
}

func (g AccessGrant) Validate() error {
	if g.TenantID == "" {
		return invalid("artifact_grant_tenant_required", "artifact grant tenant is required", nil)
	}
	if g.IssuedAt.IsZero() || g.ExpiresAt.IsZero() || !g.ExpiresAt.After(g.IssuedAt) {
		return invalid("artifact_grant_validity_required", "artifact grant requires an issue time and a later expiry", nil)
	}
	if g.ExpiresAt.Sub(g.IssuedAt) > MaximumGrantTTL {
		return invalid("artifact_grant_ttl_exceeded", "artifact grant validity exceeds the maximum grant TTL", nil)
	}
	if len(g.ReadableDigests) > 0 && g.MaximumReadBytes == 0 {
		return invalid("artifact_read_budget_required", "artifact read grant requires byte budget", nil)
	}
	if len(g.WritableNamespaces) > 0 && g.MaximumWriteBytes == 0 {
		return invalid("artifact_write_budget_required", "artifact write grant requires byte budget", nil)
	}
	return nil
}

// Active reports whether the grant is within its validity window at now.
//
// The boundary is exclusive at both ends in the same direction the byte plane
// uses: a grant is dead at its expiry instant, not one tick after it.
func (g AccessGrant) Active(now time.Time) bool {
	return !g.IssuedAt.IsZero() && !g.ExpiresAt.IsZero() && !now.Before(g.IssuedAt) && now.Before(g.ExpiresAt)
}

// RequireActive re-checks the time bound. Callers must run it per operation
// rather than once at construction: validating a grant when it is minted never
// turns an expiring grant into timeless authority.
func (g AccessGrant) RequireActive(now time.Time) error {
	if err := g.Validate(); err != nil {
		return err
	}
	if !g.Active(now) {
		return faults.New(faults.CodeDeadlineExceeded, "artifact grant is not active",
			faults.WithReason("artifact_grant_expired"), faults.WithOperation("control.artifacts"),
			faults.WithRetryPolicy(faults.NoRetry()))
	}
	return nil
}
