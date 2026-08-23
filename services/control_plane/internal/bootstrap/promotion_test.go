// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package bootstrap

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const commandPackage = "services/control_plane/cmd"

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
	forEachCommandMain(t, func(path string, source string) {
		if !strings.Contains(source, "bootstrap.Main(") {
			t.Errorf("%s does not enter through bootstrap.Main", path)
		}
	})
}

// The lifecycle constructs libs/go/CONSUMPTION.md forbids a production command
// to own. They are checked across every non-test file in the command package
// rather than main.go alone: a sibling file in the same package compiles into
// the same binary, so a rule that reads one filename is a rule about a filename
// rather than about the process.
func TestNoCommandOwnsItsOwnLifecycle(t *testing.T) {
	forbidden := map[string]string{
		"servicekit.New(":         "constructs a service directly instead of using the staged lifecycle",
		"servicekit.NewAssembly(": "assembles a service of its own",
		"production.NewBuilder(":  "composes its own production runtime instead of using bootstrap",
		"signal.Notify":           "takes signal ownership from servicekit",
	}
	forEachCommandSource(t, func(path string, source string) {
		for token, message := range forbidden {
			if strings.Contains(source, token) {
				t.Errorf("%s %s (%s)", path, message, token)
			}
		}
	})
}

// A detached goroutine is checked through the parser rather than by matching
// text. "go func(" is only the anonymous spelling; `go start()` is the same
// violation, and either spelling inside a comment is not a violation at all.
// The go statement is a syntactic form, so the parser is what actually knows.
func TestNoCommandSpawnsADetachedGoroutine(t *testing.T) {
	forEachCommandSource(t, func(path string, source string) {
		file, err := parser.ParseFile(token.NewFileSet(), path, source, parser.SkipObjectResolution)
		if err != nil {
			t.Errorf("%s does not parse: %v", path, err)
			return
		}
		ast.Inspect(file, func(node ast.Node) bool {
			if _, detached := node.(*ast.GoStmt); detached {
				t.Errorf("%s spawns a detached goroutine outside the servicekit lifecycle", path)
			}
			return true
		})
	})
}

// Every role in the profile table must have a command, and every command must
// name a role. A role with no command is a manifest entry nothing can deploy;
// a command with no role cannot be validated before it starts.
func TestEveryRoleHasACommand(t *testing.T) {
	commandRoot := commandSourceRoot(t)
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

// forEachCommandMain visits the entry point of every command. Use it only for
// properties that are genuinely about main.go, such as which factory the
// process wires.
func forEachCommandMain(t *testing.T, check func(path string, source string)) {
	t.Helper()
	commandRoot := commandSourceRoot(t)
	seen := 0
	for _, command := range commandDirectories(t, commandRoot) {
		path := filepath.Join(commandRoot, command, "main.go")
		source, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("command %q has no main.go: %v", command, err)
			continue
		}
		seen++
		check(path, string(source))
	}
	if seen == 0 {
		t.Fatal("no control-plane commands were found")
	}
}

func commandSourceRoot(t *testing.T) string {
	t.Helper()
	if runfilesRoot := os.Getenv("TEST_SRCDIR"); runfilesRoot != "" {
		workspace := os.Getenv("TEST_WORKSPACE")
		if workspace == "" {
			t.Fatal("TEST_WORKSPACE is empty under Bazel")
		}
		return filepath.Join(runfilesRoot, workspace, commandPackage)
	}
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot resolve promotion test source path")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(source), "../../cmd"))
}

// forEachCommandSource visits every non-test file that compiles into a command
// binary. Properties about what the process does belong here.
func forEachCommandSource(t *testing.T, check func(path string, source string)) {
	t.Helper()
	commandRoot := commandSourceRoot(t)
	seen := 0
	for _, command := range commandDirectories(t, commandRoot) {
		directory := filepath.Join(commandRoot, command)
		entries, err := os.ReadDir(directory)
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			name := entry.Name()
			if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				continue
			}
			path := filepath.Join(directory, name)
			source, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			seen++
			check(path, string(source))
		}
	}
	if seen == 0 {
		t.Fatal("no control-plane command sources were found")
	}
}

func commandDirectories(t *testing.T, commandRoot string) []string {
	t.Helper()
	entries, err := os.ReadDir(commandRoot)
	if err != nil {
		t.Fatal(err)
	}
	commands := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			commands = append(commands, entry.Name())
		}
	}
	return commands
}
