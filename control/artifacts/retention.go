// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package artifacts

import "time"

type RetentionPolicy struct {
	MinimumAge time.Duration
	KeepKinds  map[string]bool
}

func (p RetentionPolicy) Eligible(kind string, created, now time.Time) bool {
	return !p.KeepKinds[kind] && !created.IsZero() && !now.Before(created.Add(p.MinimumAge))
}
