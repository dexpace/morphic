package pass_test

import (
	"go/types"
	"reflect"
	"slices"
	"sync"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/internal/decls"
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
// forEachPayload names the Payload-bearing fields by hand, a coupling the
// compiler cannot check: a carrier added to the IR would otherwise be walked by
// neither the checks built on the walk nor the cases below, with the whole
// suite green. The guard holds both lists in two steps. This test holds
// encodingCarriers against the IR; TestValidate_EncodingKeyAddressesNoProperty
// holds forEachPayload against encodingCarriers by requiring a diagnostic from
// every entry.
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
			"(-declared +listed); a new one also has to be walked by forEachPayload")
}

// payloadFields returns "Owner.Field", sorted, for every struct field the IR
// declares whose type is or contains an ir.Payload.
//
// The walk starts at ir.Document and visits each distinct reflect.Type once, so
// recursive shapes terminate. The sealed TypeDef sum is reached only through an
// interface, which a static walk cannot descend into, so each concrete kind is
// walked from its own root as well, seeded from the kinds the ir sources
// declare so a new variant is covered at once (ir/nofloat_test.go walks the
// same two halves).
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

// irPackage is the ir package type-checked, loaded once per test binary.
var irPackage = sync.OnceValues(func() (*decls.Package, error) {
	return decls.Load("github.com/dexpace/morphic/ir")
})

// irDecls returns the ir package, type-checked.
func irDecls(t *testing.T) *decls.Package {
	t.Helper()
	p, err := irPackage()
	require.NoError(t, err)
	return p
}

// irTypeKinds returns every TypeKind constant the ir package declares, read
// type-checked because a list of the kinds written here is one commit away from
// disagreeing with the sum it claims to enumerate, which is the failure this
// whole file exists to catch.
//
// It reads the same declarations ir's own declaredTypeKinds does, spelled out
// again because a helper in one package's test binary is not linked into
// another's.
func irTypeKinds(t *testing.T) []ir.TypeKind {
	t.Helper()
	consts, err := irDecls(t).StringConsts("TypeKind")
	require.NoError(t, err)

	kinds := make([]ir.TypeKind, len(consts))
	for i, c := range consts {
		kinds[i] = ir.TypeKind(c.Value)
	}
	require.Len(t, kinds, irTypeDefImpls(t),
		"the sum holds one concrete type per kind, so a count that disagrees means a variant is "+
			"sealed without a TypeKind or a kind has no variant, and a walk seeded from the kinds skips it")
	return kinds
}

// irTypeDefImpls counts the concrete types that declare the sealed typeDef()
// marker, the sum's membership as the compiler sees it.
//
// It holds irTypeKinds to completeness from the other side: a walk seeded from
// the kinds skips a variant that has none, in silence. Removing a kind drops its
// constant and its marker together, so the two agree without anyone maintaining
// a number.
func irTypeDefImpls(t *testing.T) int {
	t.Helper()
	scope := irDecls(t).Types.Scope()
	impls := 0
	for _, name := range scope.Names() {
		tn, isType := scope.Lookup(name).(*types.TypeName)
		if !isType {
			continue
		}
		named, isNamed := tn.Type().(*types.Named)
		if !isNamed {
			continue
		}
		for m := range named.Methods() {
			if m.Name() == "typeDef" {
				impls++
			}
		}
	}
	require.NotZero(t, impls, "the ir sources must seal concrete types into the TypeDef sum")
	return impls
}
