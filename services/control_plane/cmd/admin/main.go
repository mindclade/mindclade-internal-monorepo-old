// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

// Command admin is the production composition root for the control-plane
// admin role. Provider construction remains service-owned; all lifecycle
// behavior flows through servicekit/production via bootstrap.
//
// The factory comes from the api package because the admin and api roles have
// identical capability profiles: they are one composition deployed twice, on
// separate listeners, not two implementations of the same thing.
//
// The PostgreSQL driver is linked here, in the only package that may decide
// which drivers this binary carries. The provider factory resolves the
// configured driver name against the registered set and fails closed when it
// is absent.
package main

import (
	_ "github.com/lib/pq"

	"go.mindclade.dev/services/control_plane/internal/bootstrap"
	"go.mindclade.dev/services/control_plane/internal/providers/api"
)

func main() {
	bootstrap.Main(bootstrap.RoleAdmin, api.NewAdminFactory())
}
