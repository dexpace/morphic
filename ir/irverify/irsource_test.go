package irverify

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// maxTypeChain bounds the walk through defined types and aliases (the
// bounded-recursion rule). Go forbids a cycle among type declarations, so
// exceeding this means the parse went wrong, not that the IR grew deep.
const maxTypeChain = 16

// typeDecls maps every type name the ir package's production sources declare at
// package level to the expression it is declared as. Function-local types are not
// package-level declarations and so cannot name a reference class.
func typeDecls(t *testing.T) map[string]ast.Expr {
	t.Helper()
	out := map[string]ast.Expr{}
	for _, path := range irSourceFiles(t) {
		f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
		require.NoError(t, err, "parsing %s", path)
		for _, decl := range f.Decls {
			gd, isGen := decl.(*ast.GenDecl)
			if !isGen || gd.Tok != token.TYPE {
				continue
			}
			for _, spec := range gd.Specs {
				ts, isType := spec.(*ast.TypeSpec)
				require.True(t, isType, "type decl spec is not a TypeSpec: %#v", spec)
				out[ts.Name.Name] = ts.Type
			}
		}
	}
	require.NotEmpty(t, out, "the ir package must declare types")
	return out
}

// namedConst is one string constant as the ir sources declare it: the Go
// identifier and the wire value it spells.
type namedConst struct {
	name  string
	value string
}

// declaredConstsOfType returns every constant of the named type the ir
// package's production sources declare, sorted by identifier.
//
// Deriving the set from the source is what TestValuePayloads_CoverTheDeclaredKinds
// holds valuePayloads to: a hand-written table is one commit away from
// disagreeing with the enum it is supposed to cover, and disagreeing silently.
// This is the same helper ir/helpers_test.go's declaredConstsOfType and
// constsOfType are — read there for the twin — copied rather than imported
// because that copy lives in package ir_test, an external test package with no
// importable path of its own.
func declaredConstsOfType(t *testing.T, typeName string) []namedConst {
	t.Helper()
	var out []namedConst
	for _, path := range irSourceFiles(t) {
		f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
		require.NoError(t, err, "parsing %s", path)
		for _, decl := range f.Decls {
			gd, isGen := decl.(*ast.GenDecl)
			if !isGen || gd.Tok != token.CONST {
				continue
			}
			out = append(out, constsOfType(t, gd, typeName)...)
		}
	}
	slices.SortFunc(out, func(a, b namedConst) int { return strings.Compare(a.name, b.name) })
	return out
}

// constsOfType returns the constants of the named type in one const group. A
// spec declaring neither type nor value repeats the previous spec, so the
// group's last explicit type carries forward; a spec with its own value
// declares its own type.
func constsOfType(t *testing.T, gd *ast.GenDecl, typeName string) []namedConst {
	t.Helper()
	var out []namedConst
	isWanted := false
	for _, spec := range gd.Specs {
		vs, isValue := spec.(*ast.ValueSpec)
		require.True(t, isValue, "const spec is not a ValueSpec: %#v", spec)
		switch {
		case vs.Type != nil:
			id, isIdent := vs.Type.(*ast.Ident)
			isWanted = isIdent && id.Name == typeName
		case len(vs.Values) > 0:
			isWanted = false
		}
		if !isWanted {
			continue
		}
		for i, name := range vs.Names {
			require.Less(t, i, len(vs.Values),
				"%s constant %s must declare its own value", typeName, name.Name)
			out = append(out, namedConst{name: name.Name, value: stringLit(t, name.Name, vs.Values[i])})
		}
	}
	return out
}

// stringLit returns the string a constant's value expression spells out.
func stringLit(t *testing.T, constName string, expr ast.Expr) string {
	t.Helper()
	lit, isLit := expr.(*ast.BasicLit)
	require.True(t, isLit, "constant %s must be declared as a string literal", constName)
	require.Equal(t, token.STRING, lit.Kind, "constant %s must be declared as a string literal", constName)
	unquoted, err := strconv.Unquote(lit.Value)
	require.NoError(t, err, "unquoting the value of %s", constName)
	return unquoted
}

// irSourceFiles lists the ir package's non-test Go files.
func irSourceFiles(t *testing.T) []string {
	t.Helper()
	return goSourceFiles(t, packageDir(t, ".."))
}

// packageDir resolves rel against this test file's own directory, so a result
// does not depend on the working directory the suite runs from.
func packageDir(t *testing.T, rel string) string {
	t.Helper()
	_, self, _, ok := runtime.Caller(0)
	require.True(t, ok, "runtime.Caller must report this test's path")
	return filepath.Join(filepath.Dir(self), rel)
}

// goSourceFiles lists dir's non-test Go files.
func goSourceFiles(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)

	var out []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		out = append(out, filepath.Join(dir, name))
	}
	require.NotEmpty(t, out, "%s must hold production Go sources", dir)
	return out
}
