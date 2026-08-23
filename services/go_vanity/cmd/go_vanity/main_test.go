// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

package main

import "testing"

func TestCanonicalDefaultConfiguration(t *testing.T) {
	for _, name := range []string{
		"LISTEN_ADDRESS",
		"METRICS_LISTEN_ADDRESS",
		"MODULE_PATH",
		"MODULE_REPO_URL",
		"DOCS_URL",
	} {
		t.Setenv(name, "")
	}
	config := configFromEnvironment()
	if config.listenAddress != ":8080" || config.metricsAddress != "127.0.0.1:9090" {
		t.Fatalf("listener defaults = %q, %q", config.listenAddress, config.metricsAddress)
	}
	if config.service.ModulePath != "go.mindclade.dev" {
		t.Fatalf("module path = %q", config.service.ModulePath)
	}
	if config.service.RepoURL != "https://github.com/mindclade/mindclade-internal-monorepo" {
		t.Fatalf("repository URL = %q", config.service.RepoURL)
	}
}
