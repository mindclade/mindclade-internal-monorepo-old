// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

// Command operator is the production composition root for the control-plane
// operator role. Provider construction remains service-owned; all lifecycle
// behavior flows through servicekit/production via bootstrap.
//
// The factory comes from the controller package because the operator and
// controller roles have identical capability profiles: they are the same
// composition claiming a different lease and reporting events under a
// different source, not two implementations of the same thing.
package main

import (
	"go.mindclade.dev/services/control_plane/internal/bootstrap"
	"go.mindclade.dev/services/control_plane/internal/providers/controller"
)

func main() {
	bootstrap.Main(bootstrap.RoleOperator, controller.NewOperatorFactory())
}
