package irverify

// This test is in the internal test package, not irverify_test, because it seeds
// the sealed-kind walk from the ir sources with the AST helpers that live here
// (irSourceFiles, in refkinds_test.go).

import (
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// TestIR_NoFloatFields walks the ir.Document type graph and fails on any float
// field: numeric IR data must use ir.BigVal decimal strings, never float32 or
// float64 (the TypeSpec Numeric lesson). The seen set bounds the walk: each
// distinct reflect.Type is visited exactly once, so recursive shapes terminate.
func TestIR_NoFloatFields(t *testing.T) {
	seen := map[reflect.Type]bool{}
	var walk func(rt reflect.Type, path string)
	walk = func(rt reflect.Type, path string) {
		if rt == nil || seen[rt] {
			return
		}
		seen[rt] = true
		switch rt.Kind() {
		case reflect.Float32, reflect.Float64:
			t.Errorf("float field reachable in IR at %s (%s): use ir.BigVal", path, rt)
		case reflect.Pointer, reflect.Slice, reflect.Array:
			walk(rt.Elem(), path+"[]")
		case reflect.Map:
			walk(rt.Key(), path+".key")
			walk(rt.Elem(), path+".val")
		case reflect.Struct:
			for i := range rt.NumField() {
				f := rt.Field(i)
				walk(f.Type, path+"."+f.Name)
			}
		}
	}

	walk(reflect.TypeOf(ir.Document{}), "Document")
	// Sealed TypeDef kinds are reached only via the interface, which reflection on
	// the static type graph cannot enumerate. Walk each concrete kind explicitly,
	// seeded from the kinds the ir sources declare so a new variant is float-checked
	// the day it is added, not the day someone remembers to extend a list here.
	for _, k := range declaredTypeKinds(t) {
		td, ok := ir.NewTypeDef(k)
		require.True(t, ok, "no concrete type registered for kind %q", k)
		rt := reflect.TypeOf(td)
		require.Equal(t, reflect.Pointer, rt.Kind(), "NewTypeDef must return a pointer for %q", k)
		walk(rt.Elem(), rt.Elem().Name())
	}
}

// declaredTypeKinds returns every TypeKind constant the ir package's production
// sources declare. ir.TypeDef is sealed, so this is the sum's full membership; the
// completeness tests in ir (typedef_completeness_test.go) hold the constants, the
// registry and the concrete types to the same set.
func declaredTypeKinds(t *testing.T) []ir.TypeKind {
	t.Helper()
	var out []ir.TypeKind
	for _, path := range irSourceFiles(t) {
		f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
		require.NoError(t, err, "parsing %s", path)
		for _, decl := range f.Decls {
			gd, isGen := decl.(*ast.GenDecl)
			if !isGen || gd.Tok != token.CONST {
				continue
			}
			out = append(out, typeKindConstsIn(t, gd)...)
		}
	}
	require.NotEmpty(t, out, "the ir sources must declare TypeKind constants")
	return out
}

// typeKindConstsIn returns the values of one const group's TypeKind constants. A
// spec declaring neither type nor value repeats the previous spec, so the group's
// last explicit type carries forward; a spec with its own value declares its own
// type.
func typeKindConstsIn(t *testing.T, gd *ast.GenDecl) []ir.TypeKind {
	t.Helper()
	var out []ir.TypeKind
	isKind := false
	for _, spec := range gd.Specs {
		vs, isValue := spec.(*ast.ValueSpec)
		require.True(t, isValue, "const spec is not a ValueSpec: %#v", spec)
		switch {
		case vs.Type != nil:
			id, isIdent := vs.Type.(*ast.Ident)
			isKind = isIdent && id.Name == "TypeKind"
		case len(vs.Values) > 0:
			isKind = false
		}
		if !isKind {
			continue
		}
		for i, name := range vs.Names {
			require.Less(t, i, len(vs.Values),
				"TypeKind constant %s must declare its own value", name.Name)
			lit, isLit := vs.Values[i].(*ast.BasicLit)
			require.True(t, isLit, "TypeKind constant %s must be a string literal", name.Name)
			require.Equal(t, token.STRING, lit.Kind,
				"TypeKind constant %s must be a string literal", name.Name)
			unquoted, err := strconv.Unquote(lit.Value)
			require.NoError(t, err, "unquoting the value of %s", name.Name)
			out = append(out, ir.TypeKind(unquoted))
		}
	}
	return out
}
