// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

// Command maintenance is the production composition root for the control-plane
// maintenance role. Provider construction remains service-owned; all lifecycle
// behavior flows through servicekit/production via bootstrap.
//
// The PostgreSQL driver is linked here, in the only package that may decide
// which drivers this binary carries. The provider factory resolves the
// configured driver name against the registered set and fails closed when it
// is absent.
package main

import (
	_ "github.com/lib/pq"

	"go.mindclade.dev/services/control_plane/internal/bootstrap"
	"go.mindclade.dev/services/control_plane/internal/providers/maintenance"
)

func main() {
	bootstrap.Main(bootstrap.RoleMaintenance, maintenance.NewMaintenanceFactory())
}
