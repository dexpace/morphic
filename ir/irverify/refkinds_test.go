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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRefKindByType_CoversEveryTypedID fails when ir declares a typed ID that
// refKindByType neither resolves nor the exclusion list below accounts for.
//
// collectRefs finds reference-bearing fields by reflection, so no field can be
// missed; refKindByType is the one piece still written by hand, and this is what
// stops a newly added registry from going silently unchecked.
func TestRefKindByType_CoversEveryTypedID(t *testing.T) {
	t.Parallel()
	// Typed IDs that no document-level registry can resolve: operations,
	// services and properties are reached by position inside their owner rather
	// than through a flat ID-keyed map (see refKind).
	notReferences := map[string]bool{"OpID": true, "ServiceID": true, "PropID": true}

	resolved := map[string]bool{}
	for rt := range refKindByType {
		resolved[rt.Name()] = true
	}
	for _, name := range declaredTypedIDs(t) {
		assert.True(t, resolved[name] || notReferences[name],
			"ir.%s is a typed ID that refKindByType does not resolve and the exclusion list does not name", name)
	}
}

// declaredTypedIDs returns the name of every `type <Name>ID string` declared in
// the ir package's production sources.
func declaredTypedIDs(t *testing.T) []string {
	t.Helper()
	var names []string
	for _, path := range irSourceFiles(t) {
		f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
		require.NoError(t, err)
		for _, decl := range f.Decls {
			names = append(names, typedIDNames(decl)...)
		}
	}
	require.NotEmpty(t, names, "the ir package must declare typed IDs")
	return names
}

// typedIDNames returns the ID-suffixed string types declared by one declaration.
func typedIDNames(decl ast.Decl) []string {
	gd, ok := decl.(*ast.GenDecl)
	if !ok || gd.Tok != token.TYPE {
		return nil
	}
	var names []string
	for _, spec := range gd.Specs {
		ts, ok := spec.(*ast.TypeSpec)
		if !ok || !strings.HasSuffix(ts.Name.Name, "ID") {
			continue
		}
		if under, ok := ts.Type.(*ast.Ident); ok && under.Name == "string" {
			names = append(names, ts.Name.Name)
		}
	}
	return names
}

// irSourceFiles lists the ir package's non-test Go files, located relative to
// this test's own path so the result does not depend on the working directory.
func irSourceFiles(t *testing.T) []string {
	t.Helper()
	_, self, _, ok := runtime.Caller(0)
	require.True(t, ok, "runtime.Caller must report this test's path")
	dir := filepath.Join(filepath.Dir(self), "..")
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
	return out
}
