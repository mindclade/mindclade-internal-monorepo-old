// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

// Deliberately still a placeholder, and a tripwire rather than a silent one.
//
// The sibling files in this package replaced their scaffolds with real cross-boundary tests
// because they had subjects: control/ingestion, control/routing and control/runtime_authority
// are implemented. control/scheduling is not. Every file in it -- admission, capacity,
// placement, pool, preemption, priority, repository, reservation, service, topology, and both
// adapters/{jobset,kueue} -- is the 9-line reservation stub:
//
//	// Package scheduling reserves the boundary defined by the production blueprint.
//	package scheduling
//	const scaffold_placement = "control/scheduling/placement.go"
//
// A scheduler test written against that would assert a constant equals itself. Worse, it would
// report as coverage: the file would look tested, and whoever implements placement would have
// no signal that nothing here checks their work.
//
// So this asserts the reason instead. The moment control/scheduling gains a real declaration,
// this fails and says what to write. That is the honest state to leave a reserved path in --
// not a passing t.Helper() that claims nothing while looking like it claims something.
package tests

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSchedulingIsStillEntirelyScaffold(t *testing.T) {
	root := filepath.Join("..", "..", "..", "control", "scheduling")
	if _, err := os.Stat(root); err != nil {
		t.Skipf("control/scheduling is absent: %v", err)
	}

	var implemented []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		// _test.go excluded: control/scheduling's own scaffold tests are themselves
		// TestScaffoldX(t) { t.Helper() } bodies, so counting them would trip this
		// immediately and for the wrong reason. Implementation is what matters here.
		if err != nil || entry.IsDir() ||
			!strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		file, parseErr := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if parseErr != nil {
			return parseErr
		}
		for _, declaration := range file.Decls {
			switch typed := declaration.(type) {
			case *ast.FuncDecl:
				implemented = append(implemented, path+":"+typed.Name.Name)
			case *ast.GenDecl:
				// A scaffold is exactly one const naming its own path. A type, an interface,
				// a var, or an import means somebody started building.
				if typed.Tok == token.TYPE || typed.Tok == token.VAR {
					implemented = append(implemented, path+":"+typed.Tok.String())
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(implemented) > 0 {
		t.Fatalf("control/scheduling is no longer scaffold -- %d real declarations, starting at %s.\n"+
			"Replace this test with real scheduler coverage: quota and fair-share admission, "+
			"placement against topology, and preemption ordering. Delete this tripwire when you do.",
			len(implemented), implemented[0])
	}
}
