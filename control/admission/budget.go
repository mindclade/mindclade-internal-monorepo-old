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
	ID        identifiers.ID
	Workspace string
	Limit     Quota
	StartsAt  time.Time
	ExpiresAt time.Time
	Version   resourceversion.Version
}

func (b Budget) Validate() error {
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
	if err := b.Version.Validate(); err != nil {
		return invalid("budget_version_invalid", "budget version is invalid", err)
	}
	return nil
}

func (b Budget) ActiveAt(now time.Time) bool {
	return !now.Before(b.StartsAt) && now.Before(b.ExpiresAt)
}

func (b Budget) clone() Budget {
	b.Limit = b.Limit.Clone()
	return b
}
