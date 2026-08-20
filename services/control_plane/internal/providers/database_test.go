// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package providers

import (
	"testing"

	_ "github.com/lib/pq"

	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/services/control_plane/internal/config"
)

func TestOpenDatabaseRejectsUnlinkedDriver(t *testing.T) {
	_, err := openDatabase(config.Settings{DatabaseDriver: "pgx", DatabaseDSN: "postgres://localhost/db"})
	if err == nil || faults.ReasonOf(err) != "database_driver_not_linked" {
		t.Fatalf("err=%v", err)
	}
}
