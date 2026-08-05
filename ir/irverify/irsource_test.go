package irverify

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
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
