// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

package main

import (
	"context"
	"testing"
)

func TestMainCompositionBuildsRunnableApplication(t *testing.T) {
	application, err := BuildApplication(context.Background())
	if err != nil {
		t.Fatalf("BuildApplication: %v", err)
	}
	if application == nil || application.Runtime == nil {
		t.Fatal("expected a configured application runtime")
	}
}
