// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package bootstrap

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// commandRoot is this package's path to the control-plane commands. The test
// reads source rather than importing the commands, because a main package
// cannot be imported and the property under test is about what the source
// wires, not about what it does at runtime.
const commandRoot = "../../cmd"

// PRODUCTION_READINESS.md requires that a promoted command not reach
// bootstrap.UnconfiguredFactory. That is a property of the source, so it is
// checked here rather than left to review: the factory fails closed with exit
// 78, and a command that still references it cannot be deployed.
func TestNoCommandUsesTheUnconfiguredFactory(t *testing.T) {
	forEachCommandSource(t, func(path string, source string) {
		if strings.Contains(source, "UnconfiguredFactory") {
			t.Errorf("%s still wires bootstrap.UnconfiguredFactory and cannot be promoted", path)
		}
	})
}

// Every command must enter through the shared lifecycle. A command that calls
// servicekit directly, or that builds a service of its own, would bypass role
// validation, staged startup, and bounded reverse shutdown.
func TestEveryCommandEntersThroughSharedBootstrap(t *testing.T) {
	forEachCommandSource(t, func(path string, source string) {
		if !strings.Contains(source, "bootstrap.Main(") {
			t.Errorf("%s does not enter through bootstrap.Main", path)
		}
		if strings.Contains(source, "servicekit.New(") {
			t.Errorf("%s constructs a service directly instead of using the staged lifecycle", path)
		}
	})
}

// Every role in the profile table must have a command, and every command must
// name a role. A role with no command is a manifest entry nothing can deploy;
// a command with no role cannot be validated before it starts.
func TestEveryRoleHasACommand(t *testing.T) {
	entries, err := os.ReadDir(commandRoot)
	if err != nil {
		t.Fatal(err)
	}
	commands := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			commands[entry.Name()] = struct{}{}
		}
	}
	for _, profile := range Profiles() {
		// Command directories use underscores where role names use hyphens.
		name := strings.ReplaceAll(profile.Role.String(), "-", "_")
		if _, found := commands[name]; !found {
			t.Errorf("role %q has no command directory cmd/%s", profile.Role, name)
		}
	}
}

func forEachCommandSource(t *testing.T, check func(path string, source string)) {
	t.Helper()
	entries, err := os.ReadDir(commandRoot)
	if err != nil {
		t.Fatal(err)
	}
	seen := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(commandRoot, entry.Name(), "main.go")
		source, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("command %q has no main.go: %v", entry.Name(), err)
			continue
		}
		seen++
		check(path, string(source))
	}
	if seen == 0 {
		t.Fatal("no control-plane commands were found")
	}
}
