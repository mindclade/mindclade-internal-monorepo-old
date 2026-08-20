// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

// Command event-dispatcher is the production composition root for its
// control-plane process role. Provider construction remains service-owned; all
// lifecycle behavior flows through servicekit/production via bootstrap.
//
// The PostgreSQL driver is linked here, in the only package that may decide
// which drivers this binary carries.
package main

import (
	_ "github.com/lib/pq"

	"go.mindclade.dev/services/control_plane/internal/bootstrap"
	"go.mindclade.dev/services/control_plane/internal/providers/dispatcher"
)

func main() {
	bootstrap.Main(bootstrap.RoleEventDispatcher, dispatcher.NewEventDispatcherFactory())
}
