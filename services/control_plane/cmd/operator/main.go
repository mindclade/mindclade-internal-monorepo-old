// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

// Command operator is the production composition root for its
// control-plane process role. Provider construction remains service-owned; all
// lifecycle behavior flows through servicekit/production via bootstrap.
package main

import "mindclade.internal/services/control_plane/internal/bootstrap"

func main() {
	bootstrap.Main(bootstrap.RoleOperator, bootstrap.UnconfiguredFactory("operator"))
}
