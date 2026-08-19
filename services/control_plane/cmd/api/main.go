// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

// Command api is the production composition root for the control-plane API
// role. Provider construction remains service-owned; all lifecycle behavior
// flows through servicekit/production via bootstrap.
package main

import (
	"go.mindclade.dev/services/control_plane/internal/bootstrap"
	"go.mindclade.dev/services/control_plane/internal/providers/api"
)

func main() {
	bootstrap.Main(bootstrap.RoleAPI, api.NewAPIFactory())
}
