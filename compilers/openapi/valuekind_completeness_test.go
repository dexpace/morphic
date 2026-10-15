// This file is a package-level suite, not a per-source-file test: it holds the
// compiler's switches over ir.ValueKind to the set the ir package declares, so it
// reads two packages' declarations rather than exercising one file's behaviour.
package openapi_test // external test package — the check reads declarations, not behaviour

import (
	"errors"
	"fmt"
	"go/ast"
	"go/constant"
	"go/types"
	"maps"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/internal/decls"
)

// modulePrefix is the import path every package of the module starts with, and
// irImportPath is the ir package's.
const (
	modulePrefix = "github.com/dexpace/morphic/"
	irImportPath = modulePrefix + "ir"
)

// loadedTree is the ir package and every package of the compiler's tree,
// production sources only, each type-checked on its own.
type loadedTree struct {
	ir       *decls.Package
	compiler []*decls.Package
}

// compilerTree loads the tree once per test binary. The pattern is relative to
// this package's directory, which is where the test runs.
var compilerTree = sync.OnceValues(func() (loadedTree, error) {
	pkgs, err := decls.LoadEach(irImportPath, "./...")
	if err != nil {
		return loadedTree{}, err
	}
	var tree loadedTree
	for _, p := range pkgs {
		if p.Types.Path() == irImportPath {
			tree.ir = p
			continue
		}
		tree.compiler = append(tree.compiler, p)
	}
	if tree.ir == nil || len(tree.compiler) == 0 {
		return loadedTree{}, errors.New("the load returned the ir package or the compiler's, not both")
	}
	return tree, nil
})

// loadTree returns the loaded tree, failing the test if it did not load.
func loadTree(t *testing.T) loadedTree {
	t.Helper()
	tree, err := compilerTree()
	require.NoError(t, err)
	return tree
}

// irDecls returns the ir package, type-checked.
func irDecls(t *testing.T) *decls.Package {
	t.Helper()
	return loadTree(t).ir
}

// irConstsOfType returns every constant the ir package declares with the named
// string type, in declaration order.
func irConstsOfType(t *testing.T, typeName string) []decls.Const {
	t.Helper()
	consts, err := irDecls(t).StringConsts(typeName)
	require.NoError(t, err)
	return consts
}

// TestValueKindSwitches_NameEveryValueKind requires every switch over
// ir.ValueKind in this compiler's sources to name every ValueKind constant the
// ir package declares.
//
// A switch ending in a guessing default absorbs a new kind as whatever the
// default asserts, and ValueKind is a bare string enum with no marker method to
// enforce completeness. Deriving both sets from the declarations makes the next
// kind, or the next switch, a red test. A switch is over ir.ValueKind by its
// tag's type; a case counts by the value it evaluates to, however spelled. An
// arm may still misclassify a kind; it may no longer omit it.
func TestValueKindSwitches_NameEveryValueKind(t *testing.T) {
	t.Parallel()
	declared := irConstsOfType(t, "ValueKind")
	require.NotEmpty(t, declared, "the ir sources must declare ValueKind constants")

	switches := valueKindSwitches(t)
	require.NotEmpty(t, switches, "found no switch over ir.ValueKind in the compiler's sources")
	for _, sw := range switches {
		sw.assertNamesEvery(t, declared)
	}
}

// valueKindSwitch is one switch over ir.ValueKind: where it is, and the
// constant values its case expressions evaluate to.
type valueKindSwitch struct {
	at     string
	values map[string]bool
}

// assertNamesEvery fails for each declared kind sw has no case for, and for
// each case value no constant declares. A case that is no constant, such as a
// call, names no value and is read as absent.
func (sw valueKindSwitch) assertNamesEvery(t *testing.T, declared []decls.Const) {
	t.Helper()
	known := make(map[string]bool, len(declared))
	for _, c := range declared {
		known[c.Value] = true
		assert.True(t, sw.values[c.Value],
			"the switch at %s has no case for ir.%s (%q): give it an arm; "+
				"the default may not be what it deserves", sw.at, c.Name, c.Value)
	}
	for _, value := range slices.Sorted(maps.Keys(sw.values)) {
		assert.True(t, known[value],
			"the switch at %s names %q, which no ir.ValueKind constant declares", sw.at, value)
	}
}

// valueKindSwitches returns every switch in the compiler's tree whose tag is an
// ir.ValueKind, by package, file and source order. Test files are not the
// compiler, and the loader reads none.
func valueKindSwitches(t *testing.T) []valueKindSwitch {
	t.Helper()
	var out []valueKindSwitch
	for _, p := range loadTree(t).compiler {
		valueKind, reaches := valueKindIn(t, p)
		if !reaches {
			continue // a package that reaches no ir cannot switch over its ValueKind
		}
		for _, file := range p.Syntax {
			for n := range ast.Preorder(file) {
				sw, isSwitch := n.(*ast.SwitchStmt)
				if !isSwitch || sw.Tag == nil || !types.Identical(p.Info.TypeOf(sw.Tag), valueKind) {
					continue
				}
				out = append(out, valueKindSwitch{at: position(p, sw), values: caseValues(t, p.Info, sw)})
			}
		}
	}
	return out
}

// valueKindIn returns ir.ValueKind as p's own types use it, and whether p
// reaches the ir package at all. Each package is loaded on its own, so the
// type p's switches have is identical only to this copy.
func valueKindIn(t *testing.T, p *decls.Package) (types.Type, bool) {
	t.Helper()
	irAsSeen, reaches := p.Imported(irImportPath)
	if !reaches {
		return nil, false
	}
	valueKind, isType := irAsSeen.Scope().Lookup("ValueKind").(*types.TypeName)
	require.True(t, isType, "%s sees no ir.ValueKind type", p.Types.Path())
	return valueKind.Type(), true
}

// position spells where sw is, as a path from the module root.
func position(p *decls.Package, sw *ast.SwitchStmt) string {
	pos := p.Fset.Position(sw.Pos())
	dir := strings.TrimPrefix(p.Types.Path(), modulePrefix)
	return fmt.Sprintf("%s:%d", path.Join(dir, filepath.Base(pos.Filename)), pos.Line)
}

// caseValues returns the string value of every constant case expression of sw.
func caseValues(t *testing.T, info *types.Info, sw *ast.SwitchStmt) map[string]bool {
	t.Helper()
	values := map[string]bool{}
	for _, stmt := range sw.Body.List {
		clause, isClause := stmt.(*ast.CaseClause)
		require.True(t, isClause, "a switch body holds only case clauses, got %T", stmt)
		for _, expr := range clause.List {
			if v := info.Types[expr].Value; v != nil && v.Kind() == constant.String {
				values[constant.StringVal(v)] = true
			}
		}
	}
	return values
}
