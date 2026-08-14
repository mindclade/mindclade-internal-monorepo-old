// Copyright 2026 Mindclade. All rights reserved.
// Confidential and proprietary.

package client

import (
	"testing"
	"time"
)

func TestConfigValidation(t *testing.T) {
	config := DefaultConfig()
	if err := config.Validate(); err != nil {
		t.Fatal(err)
	}
	config.Timeout = -time.Second
	if err := config.Validate(); err == nil {
		t.Fatal("Validate() returned nil for negative timeout")
	}
}

func TestInClusterRejectsKubeconfig(t *testing.T) {
	config := DefaultConfig()
	config.Source = SourceInCluster
	config.KubeconfigPath = "/tmp/config"
	if err := config.Validate(); err == nil {
		t.Fatal("Validate() returned nil")
	}
}
