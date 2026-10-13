// This file is a package-level suite, not a per-source-file test: it holds the
// compiler's switches over ir.ValueKind to the set the ir package declares, so it
// reads two packages' declarations rather than exercising one file's behaviour.
package openapi_test // external test package — the check reads declarations, not behaviour

import (
	"cmp"
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

// irImportPath and modulePath locate the ir package and the module that holds
// both it and the compiler.
const (
	irImportPath = "github.com/dexpace/morphic/ir"
	modulePath   = "github.com/dexpace/morphic/"
)

// loadedTree is the ir package and every package of the compiler's tree,
// production sources only, type-checked together so that ir.ValueKind is one
// type to all of them.
type loadedTree struct {
	ir       *decls.Package
	compiler []*decls.Package
}

// compilerTree loads the tree once per test binary. The pattern is relative to
// this package's directory, which is where the test runs.
var compilerTree = sync.OnceValues(func() (loadedTree, error) {
	pkgs, err := decls.LoadAll(irImportPath, "./...")
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
	valueKind, err := irDecls(t).Named("ValueKind")
	require.NoError(t, err)

	switches := valueKindSwitches(t, valueKind)
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
// ir.ValueKind, ordered by position. Test files are not the compiler, and the
// loader reads none.
func valueKindSwitches(t *testing.T, valueKind *types.Named) []valueKindSwitch {
	t.Helper()
	var out []valueKindSwitch
	for _, p := range loadTree(t).compiler {
		for _, file := range p.Syntax {
			for n := range ast.Preorder(file) {
				sw, isSwitch := n.(*ast.SwitchStmt)
				if !isSwitch || sw.Tag == nil || !types.Identical(types.Unalias(p.Info.TypeOf(sw.Tag)), valueKind) {
					continue
				}
				out = append(out, valueKindSwitch{at: position(p, sw), values: caseValues(t, p.Info, sw)})
			}
		}
	}
	slices.SortFunc(out, func(a, b valueKindSwitch) int { return cmp.Compare(a.at, b.at) })
	return out
}

// position spells where sw is, as a path from the module root.
func position(p *decls.Package, sw *ast.SwitchStmt) string {
	pos := p.Fset.Position(sw.Pos())
	dir := strings.TrimPrefix(p.Types.Path(), modulePath)
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
