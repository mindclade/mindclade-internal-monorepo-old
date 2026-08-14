// Copyright 2026 Mindclade. All rights reserved.
package postgres_test

import (
	"mindclade.internal/libs/go/coordination/cursor/postgres"
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
