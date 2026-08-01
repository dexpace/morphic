// This file is a package-level suite, not a per-source-file test: it holds the
// compiler's switch over ir.ValueKind to the set the ir package declares, so it
// reads two packages' sources rather than exercising one file's behaviour.
package openapi_test // external test package — the check is syntactic

import (
	"go/ast"
	"go/parser"
	"go/token"
	"slices"
	"strconv"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/require"
)

// valueKindSwitchFile and valueKindSwitchFunc locate the compiler's only switch
// over ir.ValueKind, addressed relative to this test file.
const (
	// The switch lives in another package now, so the path is spelled from this
	// package's directory. This suite stays here rather than following it: it
	// already reads the ir package's sources by path, it is about the compiler
	// rather than about the schema walk, and moving it would mean a second copy
	// of every ir-source reader below, shared with the unwitnessed-reason suite.
	valueKindSwitchFile = "internal/schema/compose.go"
	valueKindSwitchFunc = "enumMemberForm"
)

// TestEnumMemberForm_NamesEveryValueKind requires enumMemberForm's switch to name
// every ir.ValueKind constant the ir sources declare, in some arm.
//
// A ValueKind switch that ends in a guessing default is not a compile error and
// not a test failure: the new kind is simply absorbed, described as whatever the
// default asserts. That is how a byte-valued enum came to be described as a
// string with unnamed members. Deriving the set from the ir sources rather than
// maintaining a list here is what makes the next addition a red test — the same
// discipline the sealed TypeKind completeness check uses, applied to the sum that
// is a bare string enum and so has no marker method to enforce it.
//
// The check is syntactic, matching case expressions of the form ir.ValueX. An arm
// may still classify a kind wrongly; what it may no longer do is not mention it.
func TestEnumMemberForm_NamesEveryValueKind(t *testing.T) {
	t.Parallel()
	declared := irConstNamesOfType(t, "ValueKind")
	require.NotEmpty(t, declared, "the ir sources must declare ValueKind constants")

	named := valueKindsNamedInSwitch(t)
	require.NotEmpty(t, named, "found no ir.Value* case expression in "+valueKindSwitchFunc)

	slices.Sort(declared)
	slices.Sort(named)
	if diff := cmp.Diff(declared, named); diff != "" {
		t.Errorf("%s does not name every ir.ValueKind (-declared in ir +named in the switch):\n%s\n"+
			"give the missing kind an arm — the default refuses it as a non-member, which may not be what it deserves",
			valueKindSwitchFunc, diff)
	}
}

// valueKindsNamedInSwitch returns the ir.Value* constant names appearing as case
// expressions inside valueKindSwitchFunc, deduplicated.
func valueKindsNamedInSwitch(t *testing.T) []string {
	t.Helper()
	fn := findFuncDecl(t, valueKindSwitchFile, valueKindSwitchFunc)
	seen := map[string]bool{}
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		cc, isCase := n.(*ast.CaseClause)
		if !isCase {
			return true
		}
		for _, expr := range cc.List {
			if name, ok := irSelectorName(expr); ok {
				seen[name] = true
			}
		}
		return true
	})
	out := make([]string, 0, len(seen))
	for name := range seen {
		out = append(out, name)
	}
	return out
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

// findFuncDecl parses one file of this package and returns the named top-level
// function, failing when the file or the function has moved.
func findFuncDecl(t *testing.T, file, name string) *ast.FuncDecl {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), file, nil, parser.SkipObjectResolution)
	require.NoError(t, err, "parsing %s", file)
	for _, decl := range f.Decls {
		fn, isFunc := decl.(*ast.FuncDecl)
		if isFunc && fn.Recv == nil && fn.Name.Name == name {
			require.NotNil(t, fn.Body, "%s has no body", name)
			return fn
		}
	}
	t.Fatalf("%s declares no function %s; update this test to follow it", file, name)
	return nil
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
