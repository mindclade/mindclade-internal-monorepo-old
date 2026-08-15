// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package postgres_test

import (
	"go.mindclade.dev/libs/go/coordination/cursor/postgres"
	"strings"
	"testing"
)

func TestDDL(t *testing.T) {
	ddl, err := postgres.DDL("control.cursors")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(ddl, "PRIMARY KEY(namespace,name)") {
		t.Fatal("missing key")
	}
}
