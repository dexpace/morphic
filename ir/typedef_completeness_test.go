package ir_test

import (
	"encoding/json"
	"go/ast"
	"reflect"
	"slices"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// typeKindConst is one TypeKind constant as the ir sources declare it.
type typeKindConst struct {
	name string      // Go identifier, e.g. KindModel
	kind ir.TypeKind // declared wire value, e.g. "model"
}

// TestTypeDef_EveryDeclaredKindIsRegistered fails when a declared TypeKind has no
// newTypeDefByKind entry, is registered to a type reporting another kind, or shares
// its concrete type with another kind (one concrete struct per kind, ir-design §4).
func TestTypeDef_EveryDeclaredKindIsRegistered(t *testing.T) {
	t.Parallel()
	byConcrete := map[string]string{}
	for _, kc := range declaredTypeKinds(t) {
		td, ok := ir.NewTypeDef(kc.kind)
		require.True(t, ok, "ir.%s (%q) has no newTypeDefByKind entry: NewTypeDef "+
			"cannot build it, so TypeRegistry can never decode it", kc.name, kc.kind)
		assert.Equal(t, kc.kind, td.Kind(),
			"ir.%s (%q) is registered to a type whose Kind() disagrees", kc.name, kc.kind)
		assert.NotNil(t, td.Common(), "ir.%s (%q) must embed a TypeCommon", kc.name, kc.kind)

		name := concreteName(t, td)
		assert.NotContains(t, byConcrete, name,
			"ir.%s and ir.%s are both registered to ir.%s: one concrete struct per kind",
			kc.name, byConcrete[name], name)
		byConcrete[name] = kc.name
	}
}

// TestTypeDef_EveryDeclaredKindTagsItsJSON fails when a kind's zero value does not
// marshal with an adjacent "kind" tag, or does not survive TypeRegistry decoding.
//
// MarshalJSON is written once per concrete type in json.go and nothing requires
// one, so a new kind silently encodes without its tag — and invariant 7 (the
// document round-trips) breaks with the rest of the gate green.
func TestTypeDef_EveryDeclaredKindTagsItsJSON(t *testing.T) {
	t.Parallel()
	const id ir.TypeID = "t/x"
	for _, kc := range declaredTypeKinds(t) {
		t.Run(string(kc.kind), func(t *testing.T) {
			t.Parallel()
			td, ok := ir.NewTypeDef(kc.kind)
			require.True(t, ok, "ir.%s (%q) has no newTypeDefByKind entry", kc.name, kc.kind)

			raw, err := json.Marshal(ir.TypeRegistry{id: td})
			require.NoError(t, err)
			assert.Contains(t, string(raw), `"kind":"`+string(kc.kind)+`"`,
				"ir.%s (%q) marshals without its adjacent kind tag: give %T a MarshalJSON "+
					"calling marshalWithKind(%s, …)", kc.name, kc.kind, td, kc.name)

			var back ir.TypeRegistry
			require.NoError(t, json.Unmarshal(raw, &back),
				"ir.%s (%q) does not survive TypeRegistry decoding, encoded as %s",
				kc.name, kc.kind, raw)
			require.Len(t, back, 1)
			assert.Equal(t, kc.kind, back[id].Kind(), "ir.%s (%q) decodes as another kind",
				kc.name, kc.kind)
		})
	}
}

// TestTypeDef_EverySealedImplementationHasAKind fails when the types carrying the
// typeDef() marker and the types NewTypeDef builds are not the same set.
//
// The marker is the sum's membership as the compiler sees it, so a variant declared
// with no TypeKind constant fails here: every other check in this file starts from
// the constants and so cannot see it at all.
func TestTypeDef_EverySealedImplementationHasAKind(t *testing.T) {
	t.Parallel()
	kinds := declaredTypeKinds(t)
	registered := make([]string, 0, len(kinds))
	for _, kc := range kinds {
		td, ok := ir.NewTypeDef(kc.kind)
		require.True(t, ok, "ir.%s (%q) has no newTypeDefByKind entry", kc.name, kc.kind)
		registered = append(registered, concreteName(t, td))
	}
	slices.Sort(registered)

	assert.Empty(t, cmp.Diff(sealedTypeDefImpls(t), registered),
		"every ir type carrying the sealed typeDef() marker needs a TypeKind constant "+
			"registered to it, and no other (-marker +registered)")
}

// TestTypeDef_HandWrittenConcreteListIsComplete guards allConcreteTypeDefs
// (typedef_test.go), the one hand-written list naming the sum's members as
// concrete types. The kind lists below cannot stand in for it: a variant absent
// from every list is absent from nothing they compare against each other.
func TestTypeDef_HandWrittenConcreteListIsComplete(t *testing.T) {
	t.Parallel()
	listed := make([]string, 0, len(allConcreteTypeDefs))
	for _, td := range allConcreteTypeDefs {
		listed = append(listed, concreteName(t, td))
	}
	slices.Sort(listed)

	assert.Empty(t, cmp.Diff(sealedTypeDefImpls(t), listed),
		"allConcreteTypeDefs must name every ir type carrying the sealed typeDef() "+
			"marker, once each (-marker +listed)")
}

// TestTypeDef_HandWrittenKindListsAreComplete guards every list of the sum's kinds
// this package spells out by hand. Each one drives a test that iterates it, so a
// kind missing from a list is a kind that test silently never exercises — allKinds
// alone gates the switch-completeness and Common()-aliasing tests.
func TestTypeDef_HandWrittenKindListsAreComplete(t *testing.T) {
	t.Parallel()
	want := declaredKindValues(t)
	for _, tt := range []struct {
		list  string
		kinds []ir.TypeKind
	}{
		{list: "allKinds (typedef_test.go)", kinds: allKinds},
		{list: "sampleDocument (json_test.go)", kinds: sampleDocumentKinds(t)},
		{list: "typeDefZeroShapeTests (types_test.go)", kinds: zeroShapeKinds()},
	} {
		t.Run(tt.list, func(t *testing.T) {
			t.Parallel()
			got := make([]string, 0, len(tt.kinds))
			for _, k := range tt.kinds {
				got = append(got, string(k))
			}
			slices.Sort(got)

			assert.Empty(t, cmp.Diff(want, got), "%s must name every TypeKind the ir "+
				"sources declare, once each (-declared +listed)", tt.list)
		})
	}
}

// sampleDocumentKinds returns the kinds sampleDocument (json_test.go) builds.
//
// That fixture keys its registry by kind, so the row's "once each" is enforced
// over what the document ends up holding rather than over what the fixture spells
// out: a second entry of a kind already present collapses onto its twin at
// construction and never reaches this list.
func sampleDocumentKinds(t *testing.T) []ir.TypeKind {
	t.Helper()
	types := sampleDocument(t).Types
	out := make([]ir.TypeKind, 0, len(types))
	for _, td := range types {
		out = append(out, td.Kind())
	}
	return out
}

// zeroShapeKinds returns the kinds typeDefZeroShapeTests (types_test.go) pins.
func zeroShapeKinds() []ir.TypeKind {
	tests := typeDefZeroShapeTests()
	out := make([]ir.TypeKind, 0, len(tests))
	for _, tt := range tests {
		out = append(out, tt.td.Kind())
	}
	return out
}

// declaredKindValues returns the declared kinds' wire values, sorted.
func declaredKindValues(t *testing.T) []string {
	t.Helper()
	kinds := declaredTypeKinds(t)
	out := make([]string, 0, len(kinds))
	for _, kc := range kinds {
		out = append(out, string(kc.kind))
	}
	slices.Sort(out)
	return out
}

// concreteName returns the struct name behind a TypeDef pointer.
func concreteName(t *testing.T, td ir.TypeDef) string {
	t.Helper()
	rt := reflect.TypeOf(td)
	require.Equal(t, reflect.Pointer, rt.Kind(), "NewTypeDef must return a pointer, got %s", rt)
	return rt.Elem().Name()
}

// declaredTypeKinds returns every TypeKind constant the ir package's production
// sources declare, sorted by identifier.
//
// The set is derived from the source because any list of the kinds written by hand
// is one commit away from disagreeing with the sum it claims to enumerate, and
// disagreeing silently: that is what every test in this file exists to catch.
func declaredTypeKinds(t *testing.T) []typeKindConst {
	t.Helper()
	declared := declaredConstsOfType(t, "TypeKind")
	require.NotEmpty(t, declared, "the ir sources must declare TypeKind constants")
	out := make([]typeKindConst, 0, len(declared))
	for _, c := range declared {
		out = append(out, typeKindConst{name: c.name, kind: ir.TypeKind(c.value)})
	}
	return out
}

// sealedTypeDefImpls returns, sorted, the name of every ir type carrying the
// unexported typeDef() marker.
func sealedTypeDefImpls(t *testing.T) []string {
	t.Helper()
	var out []string
	for _, f := range parseIRSources(t) {
		for _, decl := range f.Decls {
			fd, isFunc := decl.(*ast.FuncDecl)
			if !isFunc || fd.Recv == nil || fd.Name.Name != "typeDef" {
				continue
			}
			out = append(out, receiverTypeName(t, fd))
		}
	}
	require.NotEmpty(t, out, "the ir sources must declare typeDef() markers")
	slices.Sort(out)
	return out
}

// receiverTypeName returns a method receiver's base type name, ignoring a pointer.
func receiverTypeName(t *testing.T, fd *ast.FuncDecl) string {
	t.Helper()
	require.Len(t, fd.Recv.List, 1, "method %s must declare one receiver", fd.Name.Name)
	expr := fd.Recv.List[0].Type
	if star, isStar := expr.(*ast.StarExpr); isStar {
		expr = star.X
	}
	id, isIdent := expr.(*ast.Ident)
	require.True(t, isIdent, "receiver of %s must be a named type, got %#v", fd.Name.Name, expr)
	return id.Name
}
