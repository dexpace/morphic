// This file is a package-level suite, not a per-source-file test: it holds the
// compiler's switches over ir.ValueKind to the set the ir package declares, so it
// reads two packages' sources rather than exercising one file's behaviour.
package openapi_test // external test package — the check is syntactic

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"maps"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/require"
)

// TestValueKindSwitches_NameEveryValueKind requires every switch over
// ir.ValueKind in this compiler's sources to name every ValueKind constant the
// ir sources declare.
//
// A switch ending in a guessing default absorbs a new kind as whatever the
// default asserts, and ValueKind is a bare string enum with no marker method to
// enforce completeness. Deriving both sets from the sources makes the next kind,
// or the next switch, a red test. The check is syntactic: a function holds such
// a switch when a case names a ValueKind constant.
func TestValueKindSwitches_NameEveryValueKind(t *testing.T) {
	t.Parallel()
	declared := irConstNamesOfType(t, "ValueKind")
	require.NotEmpty(t, declared, "the ir sources must declare ValueKind constants")
	slices.Sort(declared)

	switches := valueKindSwitches(t, declared)
	require.NotEmpty(t, switches, "found no switch over ir.ValueKind in the compiler's sources")
	for at, named := range switches {
		if diff := cmp.Diff(declared, named); diff != "" {
			t.Errorf("%s does not name every ir.ValueKind (-declared in ir +named in the switch):\n%s\n"+
				"give the missing kind an arm — the default may not be what it deserves", at, diff)
		}
	}
}

// valueKindSwitches returns, for each function in this package's tree that
// names an ir.ValueKind constant in a case clause, the constants it names,
// sorted and keyed by file and function. Test files are not the compiler.
func valueKindSwitches(t *testing.T, declared []string) map[string][]string {
	t.Helper()
	out := map[string][]string{}
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		for _, decl := range f.Decls {
			fn, isFunc := decl.(*ast.FuncDecl)
			if !isFunc || fn.Body == nil {
				continue
			}
			if named := valueKindsNamedIn(fn.Body, declared); len(named) > 0 {
				out[path+": "+fn.Name.Name] = named
			}
		}
		return nil
	})
	require.NoError(t, err)
	return out
}

// valueKindsNamedIn returns the ValueKind constants, of those declared, that
// body names as case expressions, sorted and deduplicated.
func valueKindsNamedIn(body *ast.BlockStmt, declared []string) []string {
	seen := map[string]bool{}
	ast.Inspect(body, func(n ast.Node) bool {
		cc, isCase := n.(*ast.CaseClause)
		if !isCase {
			return true
		}
		for _, expr := range cc.List {
			if name, ok := irSelectorName(expr); ok && slices.Contains(declared, name) {
				seen[name] = true
			}
		}
		return true
	})
	return slices.Sorted(maps.Keys(seen))
}

// irSelectorName returns the selector name of an expression of the form
// ir.Something, reporting ok=false for anything else.
func irSelectorName(expr ast.Expr) (string, bool) {
	sel, isSel := expr.(*ast.SelectorExpr)
	if !isSel {
		return "", false
	}
	pkg, isIdent := sel.X.(*ast.Ident)
	if !isIdent || pkg.Name != "ir" {
		return "", false
	}
	return sel.Sel.Name, true
}

// irConst is one declared constant of the ir package: its Go identifier and its
// string value.
type irConst struct {
	name  string
	value string
}

// irConstNamesOfType returns the Go identifiers of every ir constant declared
// with the named type.
func irConstNamesOfType(t *testing.T, typeName string) []string {
	t.Helper()
	consts := irConstsOfType(t, typeName)
	out := make([]string, 0, len(consts))
	for _, c := range consts {
		out = append(out, c.name)
	}
	return out
}

// irConstsOfType returns every constant the ir package's production sources
// declare with the named type, in file-then-source order.
func irConstsOfType(t *testing.T, typeName string) []irConst {
	t.Helper()
	var out []irConst
	for _, path := range irSourceFiles(t) {
		f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
		require.NoError(t, err, "parsing %s", path)
		for _, decl := range f.Decls {
			gd, isGen := decl.(*ast.GenDecl)
			if !isGen || gd.Tok != token.CONST {
				continue
			}
			out = append(out, constsInGroup(t, gd, typeName)...)
		}
	}
	return out
}

// constsInGroup returns one const group's constants of the named type. A spec
// declaring neither type nor value repeats the previous spec, so the group's last
// explicit type carries forward; a spec with a value of its own declares its own
// type.
func constsInGroup(t *testing.T, gd *ast.GenDecl, typeName string) []irConst {
	t.Helper()
	var out []irConst
	wanted := false
	for _, spec := range gd.Specs {
		vs, isValue := spec.(*ast.ValueSpec)
		require.True(t, isValue, "const spec is not a ValueSpec: %#v", spec)
		switch {
		case vs.Type != nil:
			id, isIdent := vs.Type.(*ast.Ident)
			wanted = isIdent && id.Name == typeName
		case len(vs.Values) > 0:
			wanted = false
		}
		if !wanted {
			continue
		}
		out = append(out, stringLiteralConsts(t, vs, typeName)...)
	}
	return out
}

// stringLiteralConsts returns a value spec's string-literal constants.
func stringLiteralConsts(t *testing.T, vs *ast.ValueSpec, typeName string) []irConst {
	t.Helper()
	out := make([]irConst, 0, len(vs.Names))
	for i, name := range vs.Names {
		require.Less(t, i, len(vs.Values), "%s constant %s must declare its own value", typeName, name.Name)
		lit, isLit := vs.Values[i].(*ast.BasicLit)
		require.True(t, isLit && lit.Kind == token.STRING,
			"%s constant %s must be a string literal", typeName, name.Name)
		unquoted, err := strconv.Unquote(lit.Value)
		require.NoError(t, err, "unquoting the value of %s", name.Name)
		out = append(out, irConst{name: name.Name, value: unquoted})
	}
	return out
}
