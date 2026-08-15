// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package postgres_test

import (
	"mindclade.internal/libs/go/coordination/workqueue/postgres"
	"strings"
	"testing"
)

func TestDDL(t *testing.T) {
	ddl, err := postgres.DDL("control.work_items")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(ddl, "SKIP LOCKED") && strings.Contains(ddl, "CREATE TABLE") == false {
		t.Fatal("invalid DDL")
	}
}
