// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

// Command ingestion-controller is the production composition root for its
// control-plane process role. Provider construction remains service-owned; all
// lifecycle behavior flows through servicekit/production via bootstrap.
package main

import "mindclade.internal/services/control_plane/internal/bootstrap"

func main() {
	bootstrap.Main(bootstrap.RoleIngestionController, bootstrap.UnconfiguredFactory("ingestion-controller"))
}
