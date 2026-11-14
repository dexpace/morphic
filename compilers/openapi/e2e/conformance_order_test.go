// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// conformanceSource parses conformance_test.go, with the functions it declares by
// name.
func conformanceSource(t *testing.T) map[string]*ast.FuncDecl {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), "conformance_test.go", nil, 0)
	require.NoError(t, err)
	funcs := map[string]*ast.FuncDecl{}
	for _, decl := range file.Decls {
		if fn, isFunc := decl.(*ast.FuncDecl); isFunc && fn.Recv == nil {
			funcs[fn.Name.Name] = fn
		}
	}
	return funcs
}

// readsGoldenNames reports whether node, or a function of funcs it calls at any
// depth, holds a string naming a golden: the reading of the corpus directory's
// goldens that a run rewriting them under -update races with.
func readsGoldenNames(node ast.Node, funcs map[string]*ast.FuncDecl) bool {
	seen := map[string]bool{}
	found := false
	var visit func(ast.Node)
	visit = func(n ast.Node) {
		ast.Inspect(n, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.BasicLit:
				if text, err := strconv.Unquote(x.Value); err == nil && strings.Contains(text, ".golden") {
					found = true
				}
			case *ast.CallExpr:
				callee, isIdent := x.Fun.(*ast.Ident)
				if !isIdent {
					return true
				}
				if fn, declared := funcs[callee.Name]; declared && !seen[callee.Name] {
					seen[callee.Name] = true
					visit(fn)
				}
			}
			return true
		})
	}
	visit(node)
	return found
}

// TestConformance_ComparesGoldensAfterTheSpecsThatWriteThem holds the order a
// first -update over a new case needs, which no run of the corpus shows by
// passing: the specs run inside one t.Run group that returns once they have
// written their goldens, and the corpus's specs are compared with its goldens
// after it, in TestConformance. Run beside them, the comparison reads the
// directory before they have written, and fails a correct update.
func TestConformance_ComparesGoldensAfterTheSpecsThatWriteThem(t *testing.T) {
	t.Parallel()
	funcs := conformanceSource(t)
	conformance, declared := funcs["TestConformance"]
	require.True(t, declared, "TestConformance is declared in conformance_test.go")

	specsAt := -1
	for i, stmt := range conformance.Body.List {
		expr, isExpr := stmt.(*ast.ExprStmt)
		if !isExpr {
			continue
		}
		call, isCall := expr.X.(*ast.CallExpr)
		if !isCall || len(call.Args) != 2 {
			continue
		}
		if name, isLit := call.Args[0].(*ast.BasicLit); isLit && name.Value == `"specs"` {
			specsAt = i
		}
	}
	require.GreaterOrEqual(t, specsAt, 0, `the specs run in a t.Run("specs", ...) group of TestConformance`)

	after := &ast.BlockStmt{List: conformance.Body.List[specsAt+1:]}
	assert.NotEmpty(t, after.List, "TestConformance does something after the group returns")
	assert.True(t, readsGoldenNames(after, funcs), "what follows the group compares the corpus with its goldens")
}

// TestConformance_TableNamesEveryCorpusSpecReadsNoGolden holds the other half
// of the same order: the test that holds the table to the corpus's specs,
// which runs beside the group that writes goldens, reads no golden's name.
func TestConformance_TableNamesEveryCorpusSpecReadsNoGolden(t *testing.T) {
	t.Parallel()
	funcs := conformanceSource(t)
	table, declared := funcs["TestConformance_TableNamesEveryCorpusSpec"]
	require.True(t, declared, "TestConformance_TableNamesEveryCorpusSpec is declared in conformance_test.go")
	assert.False(t, readsGoldenNames(table, funcs),
		"the table test reads the corpus's specs and no golden, so a golden being written cannot fail it")
}
