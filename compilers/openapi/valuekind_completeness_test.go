// This file is a package-level suite, not a per-source-file test: it holds the
// compiler's switch over ir.ValueKind to the set the ir package declares, so it
// reads two packages' declarations rather than exercising one file's behaviour.
package openapi_test // external test package — the check reads declarations, not behaviour

import (
	"go/ast"
	"go/constant"
	"go/types"
	"maps"
	"slices"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/internal/decls"
)

// valueKindSwitchPkg and valueKindSwitchFunc locate the compiler's only switch
// over ir.ValueKind. Naming the package rather than a file lets the function
// move within it without this test noticing.
const (
	valueKindSwitchPkg  = "github.com/dexpace/morphic/compilers/openapi/internal/schema"
	valueKindSwitchFunc = "enumMemberForm"
)

// irImportPath is the ir package's import path, which isIRNamed matches on
// rather than on type identity.
const irImportPath = "github.com/dexpace/morphic/ir"

// irPackage and switchPackage are the two packages this file reads,
// type-checked, each loaded once per test binary.
var (
	irPackage = sync.OnceValues(func() (*decls.Package, error) {
		return decls.Load(irImportPath)
	})
	switchPackage = sync.OnceValues(func() (*decls.Package, error) {
		return decls.Load(valueKindSwitchPkg)
	})
)

// TestEnumMemberForm_NamesEveryValueKind requires enumMemberForm's switch to name
// every ir.ValueKind constant the ir sources declare.
//
// A ValueKind switch ending in a guessing default is neither a compile error nor
// a test failure: a new kind is absorbed and described as whatever the default
// asserts. ValueKind is a bare string enum with no marker method to enforce
// completeness, so deriving the set from the ir sources is what makes the next
// addition a red test.
//
// A case counts by the constant value it evaluates to, however it is spelled. An
// arm may still classify a kind wrongly; it may no longer omit it.
func TestEnumMemberForm_NamesEveryValueKind(t *testing.T) {
	t.Parallel()
	declared := irConstsOfType(t, "ValueKind")
	require.NotEmpty(t, declared, "the ir sources must declare ValueKind constants")

	named := valueKindsNamedInSwitch(t)
	require.NotEmpty(t, named, "found no ir.ValueKind case expression in "+valueKindSwitchFunc)

	declaredValues := make([]string, len(declared))
	for i, c := range declared {
		declaredValues[i] = c.Value
		assert.Contains(t, named, c.Value,
			"%s names no case for ir.%s (%q): give it an arm — the default refuses it as a "+
				"non-member, which may not be what it deserves", valueKindSwitchFunc, c.Name, c.Value)
	}
	for _, value := range named {
		assert.Contains(t, declaredValues, value,
			"%s names %q, which no ir.ValueKind constant declares", valueKindSwitchFunc, value)
	}
}

// valueKindsNamedInSwitch returns, sorted and deduplicated, the values of the
// constants that case expressions evaluate to in the switches over ir.ValueKind
// inside valueKindSwitchFunc.
func valueKindsNamedInSwitch(t *testing.T) []string {
	t.Helper()
	p, err := switchPackage()
	require.NoError(t, err)
	fn, err := p.FuncDecl(valueKindSwitchFunc)
	require.NoError(t, err, "update this test to follow the function")
	require.NotNil(t, fn.Body, "%s has no body", valueKindSwitchFunc)

	seen := map[string]bool{}
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		sw, isSwitch := n.(*ast.SwitchStmt)
		if isSwitch && sw.Tag != nil && isIRNamed(p.Info.TypeOf(sw.Tag), "ValueKind") {
			for _, value := range caseValues(t, p.Info, sw) {
				seen[value] = true
			}
		}
		return true
	})
	return slices.Sorted(maps.Keys(seen))
}

// caseValues returns the string value of every constant case expression of sw.
// A case that is not a constant, such as a call, names no value and is skipped.
func caseValues(t *testing.T, info *types.Info, sw *ast.SwitchStmt) []string {
	t.Helper()
	var values []string
	for _, stmt := range sw.Body.List {
		clause, isClause := stmt.(*ast.CaseClause)
		require.True(t, isClause, "a switch body holds only case clauses, got %T", stmt)
		for _, expr := range clause.List {
			if v := info.Types[expr].Value; v != nil && v.Kind() == constant.String {
				values = append(values, constant.StringVal(v))
			}
		}
	}
	return values
}

// isIRNamed reports whether typ is the type the ir package declares as name. It
// compares import path and name rather than identity: the switch's package sees
// ir through export data, so its types are not the objects irDecls loads.
func isIRNamed(typ types.Type, name string) bool {
	named, isNamed := types.Unalias(typ).(*types.Named)
	if !isNamed {
		return false
	}
	obj := named.Obj()
	return obj.Pkg() != nil && obj.Pkg().Path() == irImportPath && obj.Name() == name
}

// irDecls returns the ir package, type-checked.
func irDecls(t *testing.T) *decls.Package {
	t.Helper()
	p, err := irPackage()
	require.NoError(t, err)
	return p
}

// irConstsOfType returns every constant the ir package declares with the named
// string type, in declaration order.
func irConstsOfType(t *testing.T, typeName string) []decls.Const {
	t.Helper()
	consts, err := irDecls(t).StringConsts(typeName)
	require.NoError(t, err)
	return consts
}
