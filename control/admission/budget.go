// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package admission

import (
	"time"

	"go.mindclade.dev/libs/go/identifiers"
	"go.mindclade.dev/libs/go/resourceversion"
)

// Budget is one immutable-versioned quota window. A new window receives a new budget ID.
type Budget struct {
	ID        identifiers.ID          `json:"id"`
	Workspace string                  `json:"workspace"`
	Limit     Quota                   `json:"limit"`
	StartsAt  time.Time               `json:"starts_at"`
	ExpiresAt time.Time               `json:"expires_at"`
	Version   resourceversion.Version `json:"resource_version"`
}

func (b Budget) Validate() error {
	if err := b.validateBody(); err != nil {
		return err
	}
	if err := b.Version.Validate(); err != nil {
		return invalid("budget_version_invalid", "budget version is invalid", err)
	}
	if !b.Version.Digest().Equal(budgetDigest(b)) {
		return invalid("budget_version_digest_mismatch", "budget version does not seal its content", nil)
	}
	return nil
}

func (b Budget) validateBody() error {
	if err := validateID(b.ID, "budget", "budget_id"); err != nil {
		return err
	}
	if err := validateName(b.Workspace, "workspace"); err != nil {
		return err
	}
	if err := b.Limit.Validate(true); err != nil {
		return err
	}
	if b.StartsAt.IsZero() || !b.ExpiresAt.After(b.StartsAt) {
		return invalid("budget_window_invalid", "budget window is invalid", nil)
	}
	return nil
}

// Seal binds the budget identity, workspace, quota, and window to generation.
func (b Budget) Seal(generation uint64) (Budget, error) {
	if err := b.validateBody(); err != nil {
		return Budget{}, err
	}
	b.Limit = b.Limit.Clone()
	version, err := resourceversion.New(generation, budgetDigest(b))
	if err != nil {
		return Budget{}, invalid("budget_version_invalid", "budget version is invalid", err)
	}
	b.Version = version
	return b, nil
}

func budgetDigest(b Budget) identifiers.Digest {
	return identifiers.SHA256String(canonicalJoin(
		"gateway-budget-v1", b.ID.String(), b.Workspace, b.Limit.canonical(),
		b.StartsAt.UTC().Format(time.RFC3339Nano), b.ExpiresAt.UTC().Format(time.RFC3339Nano),
	))
}

func (b Budget) ActiveAt(now time.Time) bool {
	return !now.Before(b.StartsAt) && now.Before(b.ExpiresAt)
}

func (b Budget) clone() Budget {
	b.Limit = b.Limit.Clone()
	return b
}
