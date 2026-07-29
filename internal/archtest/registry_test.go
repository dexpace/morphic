package archtest_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// registryOwners are the packages permitted to write an ir.TypeRegistry
// directly, as repo-relative directory prefixes.
//
// compilers/compile owns the registry and the coordinate map that make
// invariant 3 (stable IDs; one node per source coordinate) true. ir declares the
// type, and ir/irtest builds registries as fixtures.
var registryOwners = []string{"compilers/compile", "ir"}

// TestRegistryWrites_StayInsideTheFramework asserts that no production package
// outside registryOwners constructs or writes into an ir.TypeRegistry.
//
// This is the rule the compilers/compile package boundary exists to express: a
// 120-line package earns its own directory only because "no one outside writes
// this map" is inexpressible without an outside. Every compiler reaching the
// registry through compile.Types is what keeps one-node-per-coordinate a
// mechanical property rather than a convention each compiler re-honours.
//
// The check is syntactic — the import graph test parses imports only, and this
// reuses that machinery rather than standing up a type checker. It matches two
// shapes: a composite literal of ir.TypeRegistry, and an assignment whose target
// indexes a .Types selector. A compiler that renamed its registry field would
// slip past the second, which is why the first exists: building one is how a
// package would obtain a registry to write in the first place.
func TestRegistryWrites_StayInsideTheFramework(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)

	var offenders []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return skipUninterestingDir(root, path, d)
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		require.NoError(t, relErr)
		if hasPrefixDir(filepath.ToSlash(rel), registryOwners) {
			return nil
		}
		offenders = append(offenders, registryWrites(t, path, filepath.ToSlash(rel))...)
		return nil
	})
	require.NoError(t, err)

	assert.Empty(t, offenders,
		"only %v may write an ir.TypeRegistry; everything else goes through compile.Types",
		registryOwners)
}

// skipUninterestingDir prunes directories that carry no production Go code.
func skipUninterestingDir(root, path string, d fs.DirEntry) error {
	if path == root {
		return nil
	}
	switch d.Name() {
	case ".git", "testdata", "docs", "scripts", ".claude", ".github":
		return fs.SkipDir
	}
	return nil
}

// hasPrefixDir reports whether rel sits inside one of dirs, matching on
// path-segment boundaries so "irverify" does not match the "ir" owner.
func hasPrefixDir(rel string, dirs []string) bool {
	for _, d := range dirs {
		if rel == d || strings.HasPrefix(rel, d+"/") {
			return true
		}
	}
	return false
}

// registryWrites returns a description of every registry construction or write
// in the file at path.
func registryWrites(t *testing.T, path, rel string) []string {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	require.NoError(t, err)

	var found []string
	ast.Inspect(file, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.CompositeLit:
			if isSelector(node.Type, "ir", "TypeRegistry") {
				found = append(found, rel+": constructs an ir.TypeRegistry")
			}
		case *ast.AssignStmt:
			for _, lhs := range node.Lhs {
				if indexesTypesField(lhs) {
					found = append(found, rel+": assigns into a .Types registry")
				}
			}
		default:
		}
		return true
	})
	return found
}

// isSelector reports whether e is the qualified identifier pkg.name.
func isSelector(e ast.Expr, pkg, name string) bool {
	sel, ok := e.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != name {
		return false
	}
	id, ok := sel.X.(*ast.Ident)
	return ok && id.Name == pkg
}

// indexesTypesField reports whether e indexes an expression whose final selector
// is Types — the ir.Document and lowerer field name for the registry.
func indexesTypesField(e ast.Expr) bool {
	idx, ok := e.(*ast.IndexExpr)
	if !ok {
		return false
	}
	sel, ok := idx.X.(*ast.SelectorExpr)
	return ok && sel.Sel.Name == "Types"
}
