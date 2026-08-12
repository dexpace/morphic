package pass_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// payloadType is the node whose carriers this file enumerates.
var payloadType = reflect.TypeFor[ir.Payload]()

const (
	// maxTypeGraphDepth bounds the walk over the IR's static type graph and
	// maxFieldTypeDepth the walk through one field's type constructors (the
	// bounded-recursion rule). Each distinct reflect.Type is visited once, so the
	// seen set already terminates the first, and a field type is a finite tower of
	// pointers, slices and maps — except that `type T *T` is legal Go, which is
	// what the second counter is for. Reaching either cap means the walk stopped
	// being a walk over the IR, so it fails rather than truncating.
	maxTypeGraphDepth = 512
	maxFieldTypeDepth = 32
)

// TestEncodingCarriers_NameEveryPayloadFieldInTheIR fails when the IR declares a
// field carrying an ir.Payload that encodingCarriers does not name.
//
// checkEncodingKeys reaches every Content by naming the Payload-bearing fields by
// hand, because nothing in a Payload's Go type says who owns one. Naming them
// costs a coupling the compiler cannot check, and this is what checks it: a
// fourth carrier added to the IR would otherwise be walked by neither the check
// nor the cases below, its encoding keys resolved against nothing, with the whole
// suite green.
//
// The guard holds both lists at once, in two steps. Here it holds
// encodingCarriers against the IR; TestValidate_EncodingKeyAddressesNoProperty
// then holds checkEncodingKeys against encodingCarriers, by requiring a
// diagnostic from every entry. So a carrier added to the IR reddens this test,
// and adding it here reddens that one until checkEncodingKeys walks it too.
func TestEncodingCarriers_NameEveryPayloadFieldInTheIR(t *testing.T) {
	t.Parallel()
	carriers := encodingCarriers()
	listed := make([]string, 0, len(carriers))
	for _, c := range carriers {
		listed = append(listed, c.field)
	}
	slices.Sort(listed)

	found := payloadFields(t)
	require.NotEmpty(t, found, "the walk found no ir.Payload field at all, so it reached "+
		"nothing and proves nothing about the ones encodingCarriers names")
	assert.Empty(t, cmp.Diff(found, listed),
		"encodingCarriers must name every ir field that carries an ir.Payload, once each "+
			"(-declared +listed); a new one also has to be walked by checkEncodingKeys")
}

// payloadFields returns "Owner.Field", sorted, for every struct field the IR
// declares whose type is or contains an ir.Payload.
//
// The walk starts at ir.Document and visits each distinct reflect.Type once, so
// recursive shapes terminate. The sealed TypeDef sum is reached only through an
// interface, which a walk over the static type graph cannot descend into, so each
// concrete kind is walked from its own root as well — seeded from the kinds the
// ir sources declare, so a variant is covered the day it is added rather than the
// day someone remembers a list here (ir/nofloat_test.go walks the same two halves
// for the same reason).
func payloadFields(t *testing.T) []string {
	t.Helper()
	var found []string
	seen := map[reflect.Type]bool{}

	var walk func(rt reflect.Type, depth int)
	walk = func(rt reflect.Type, depth int) {
		require.Less(t, depth, maxTypeGraphDepth, "the IR type graph nests past %d", maxTypeGraphDepth)
		if seen[rt] {
			return
		}
		seen[rt] = true
		switch rt.Kind() {
		case reflect.Pointer, reflect.Slice, reflect.Array:
			walk(rt.Elem(), depth+1)
		case reflect.Map:
			walk(rt.Key(), depth+1)
			walk(rt.Elem(), depth+1)
		case reflect.Struct:
			for f := range rt.Fields() {
				if carriesPayload(t, f.Type, 0) {
					found = append(found, rt.Name()+"."+f.Name)
				}
				walk(f.Type, depth+1)
			}
		default:
			// A leaf: no other kind has a component type to descend into.
		}
	}

	walk(reflect.TypeFor[ir.Document](), 0)
	for _, kind := range irTypeKinds(t) {
		td, ok := ir.NewTypeDef(kind)
		require.True(t, ok, "no concrete type is registered for kind %q", kind)
		rt := reflect.TypeOf(td)
		require.Equal(t, reflect.Pointer, rt.Kind(), "NewTypeDef must return a pointer for %q", kind)
		walk(rt.Elem(), 0)
	}
	slices.Sort(found)
	return found
}

// carriesPayload reports whether a field's type is an ir.Payload, or a pointer,
// slice, array or map of one.
//
// It does not descend into structs. A field whose type merely reaches a Payload
// further down is the spine leading to a carrier — Document.Services and
// Document.Messages between them reach every one of today's — and naming the
// spine would name most of the IR.
func carriesPayload(t *testing.T, rt reflect.Type, depth int) bool {
	t.Helper()
	require.Less(t, depth, maxFieldTypeDepth, "resolving a field type exceeded %d steps", maxFieldTypeDepth)
	if rt == payloadType {
		return true
	}
	switch rt.Kind() {
	case reflect.Pointer, reflect.Slice, reflect.Array:
		return carriesPayload(t, rt.Elem(), depth+1)
	case reflect.Map:
		return carriesPayload(t, rt.Key(), depth+1) || carriesPayload(t, rt.Elem(), depth+1)
	default:
		return false // a struct is a carrier's owner, not a carrier
	}
}

// irTypeKinds returns every TypeKind constant the ir package's production sources
// declare. Derived from the source because a list of the kinds written here is
// one commit away from disagreeing with the sum it claims to enumerate, which is
// the failure this whole file exists to catch.
//
// It reads the same declarations ir's own declaredTypeKinds does, spelled out
// again because a helper in one package's test binary is not linked into
// another's.
func irTypeKinds(t *testing.T) []ir.TypeKind {
	t.Helper()
	var kinds []ir.TypeKind
	for _, path := range irSourcePaths(t) {
		f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
		require.NoError(t, err, "parsing %s", path)
		for _, decl := range f.Decls {
			gd, isGen := decl.(*ast.GenDecl)
			if !isGen || gd.Tok != token.CONST {
				continue
			}
			kinds = append(kinds, typeKindsIn(t, gd)...)
		}
	}
	require.Len(t, kinds, irTypeDefImpls(t),
		"the sum holds one concrete type per kind, so a count that disagrees means this parse "+
			"stopped seeing every constant rather than that the IR changed")
	return kinds
}

// irTypeDefImpls counts the concrete types the ir sources seal into the TypeDef
// sum, by the typeDef() marker methods that seal them.
//
// It reads the same sources by a different route, and exists to hold the other
// reading to completeness. irTypeKinds finding constants proves its parse ran,
// not that it saw them all, and a walk seeded from a subset skips whole concrete
// kinds in silence — the failure this file exists to catch, one level up in the
// machinery catching it. Removing a kind drops its constant and its marker
// together, so the two agree without anyone maintaining a number.
func irTypeDefImpls(t *testing.T) int {
	t.Helper()
	impls := 0
	for _, path := range irSourcePaths(t) {
		f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
		require.NoError(t, err, "parsing %s", path)
		for _, decl := range f.Decls {
			fd, isFunc := decl.(*ast.FuncDecl)
			if isFunc && fd.Recv != nil && fd.Name.Name == "typeDef" {
				impls++
			}
		}
	}
	require.NotZero(t, impls, "the ir sources must seal concrete types into the TypeDef sum")
	return impls
}

// typeKindsIn returns the TypeKind constants one const group declares. A spec
// naming neither type nor value repeats the previous one, so the group's last
// explicit type carries forward; a spec with a value of its own declares its own
// type.
func typeKindsIn(t *testing.T, gd *ast.GenDecl) []ir.TypeKind {
	t.Helper()
	var kinds []ir.TypeKind
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
			require.Less(t, i, len(vs.Values), "TypeKind constant %s must declare its own value", name.Name)
			kinds = append(kinds, ir.TypeKind(stringLit(t, name.Name, vs.Values[i])))
		}
	}
	return kinds
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

// irSourcePaths lists the ir package's non-test Go files, resolved against this
// file's own directory so the result does not depend on the working directory the
// suite runs from.
func irSourcePaths(t *testing.T) []string {
	t.Helper()
	_, self, _, ok := runtime.Caller(0)
	require.True(t, ok, "runtime.Caller must report this test's path")
	matches, err := filepath.Glob(filepath.Join(filepath.Dir(self), "..", "ir", "*.go"))
	require.NoError(t, err)

	paths := make([]string, 0, len(matches))
	for _, path := range matches {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		paths = append(paths, path)
	}
	require.NotEmpty(t, paths, "the ir package must hold production Go sources")
	return paths
}
