// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package migrate

import "time"

// Applied records one immutable database migration receipt.
type Applied struct {
	Version   uint64
	Name      string
	Checksum  string
	AppliedAt time.Time
}

// Plan is a deterministic database-to-manifest comparison.
type Plan struct {
	Applied []Applied
	Pending []Migration
}

func (plan Plan) CurrentVersion() uint64 {
	if len(plan.Applied) == 0 {
		return 0
	}
	return plan.Applied[len(plan.Applied)-1].Version
}
func (plan Plan) TargetVersion() uint64 {
	if len(plan.Pending) > 0 {
		return plan.Pending[len(plan.Pending)-1].Version
	}
	return plan.CurrentVersion()
}
