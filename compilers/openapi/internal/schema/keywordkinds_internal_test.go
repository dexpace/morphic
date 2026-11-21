// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"encoding/json/jsontext"
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/provenance"
	"github.com/dexpace/morphic/compilers/openapi/internal/service"
	"github.com/dexpace/morphic/compilers/openapi/internal/service/servicetest"
)

// TestKeywordKinds_ListsEveryValueBoundAndTheContentVocabulary holds the
// table's rows to the keywords annotation.Constraints and scalarEncoding read,
// so a bound added to one list and not to the table, or the reverse, fails here.
func TestKeywordKinds_ListsEveryValueBoundAndTheContentVocabulary(t *testing.T) {
	t.Parallel()
	want := slices.Sorted(slices.Values(slices.Concat(valueConstraintKeywords, contentKeywords)))
	assert.Equal(t, want, slices.Sorted(maps.Keys(keywordKinds)))
}

// TestBoundsFor_ReadsEachBoundForTheKindsItAppliesTo holds the table, the groups
// boundsFor maps kinds to and the groups annotation.Constraints reads to one
// another: each value bound is read by a reader of the kinds the table gives
// it, and by no reader of every other kind.
func TestBoundsFor_ReadsEachBoundForTheKindsItAppliesTo(t *testing.T) {
	t.Parallel()
	for _, keyword := range valueConstraintKeywords {
		literal := "2"
		if keyword == "pattern" {
			literal = "'^a'"
		}
		s := openapitest.SchemaFromYAML(t.Context(), t, keyword+": "+literal+"\n").GetSchema()
		kinds := keywordKinds[keyword]
		read, _, _ := annotation.Constraints(StandaloneSchema(s), boundsFor(kinds), false, "/p", provenance.Doc{})
		assert.NotNil(t, read, keyword)
		unread, _, _ := annotation.Constraints(StandaloneSchema(s), boundsFor(anyInstance&^kinds), false, "/p", provenance.Doc{})
		assert.Nil(t, unread, keyword)
	}
}

// TestDeclaredInstanceTypes_ReadsTheTypesASchemaDeclares pins what a position
// reads bounds for: the kinds of the types it declares, null aside, so none
// where it declares null alone, and every kind but null where it declares none
// or a spelling no dialect defines.
func TestDeclaredInstanceTypes_ReadsTheTypesASchemaDeclares(t *testing.T) {
	t.Parallel()
	for body, want := range map[string]kindSet{
		"description: d\n":                 anyInstance,
		"type: 'null'\n":                   0,
		"type: ['null']\n":                 0,
		"type: strin\n":                    anyInstance,
		"type: [string, 'null']\n":         kindString,
		"type: [integer, object]\n":        kindInteger | kindObject,
		"type: [number, array, boolean]\n": kindNumber | kindArray | kindBoolean,
	} {
		s := openapitest.SchemaFromYAML(t.Context(), t, body).GetSchema()
		assert.Equal(t, want, declaredInstanceTypes(StandaloneSchema(s)), body)
	}
}

// TestTypeKinds_ReadsOneListedType pins a variant's kinds: its type's, and
// every kind but null for a spelling no dialect defines.
func TestTypeKinds_ReadsOneListedType(t *testing.T) {
	t.Parallel()
	assert.Equal(t, kindString, typeKinds("string"))
	assert.Equal(t, kindNumber, typeKinds("number"))
	assert.Equal(t, anyInstance, typeKinds("strin"))
}

// TestTaken_AKeywordTheTableDoesNotListAppliesToEveryKind pins the default of
// taken: a keyword applying to no kind in particular is read by every reader.
func TestTaken_AKeywordTheTableDoesNotListAppliesToEveryKind(t *testing.T) {
	t.Parallel()
	assert.True(t, taken(0, "description"))
	assert.False(t, taken(kindString|kindObject, "minimum"))
	assert.True(t, taken(kindInteger, "minimum"))
}

// TestWritesBound_FindsABoundOnTheSchemaOrAnInlineEntry pins when a schema's
// bounds are read at all: a value or collection bound on the schema, on an inline
// entry or on an entry of a nested allOf counts, wherever the entry sits. One
// beside an entry's own $ref is that entry's, and does not.
func TestWritesBound_FindsABoundOnTheSchemaOrAnInlineEntry(t *testing.T) {
	t.Parallel()
	const obj = "{$ref: '#/components/schemas/Obj'}"
	lowerer, _ := LoweredFor(t, openapitest.ComponentSpec("    Obj: {type: object}\n"))
	for _, tc := range []struct {
		name, body string
		want       bool
	}{
		{"no bound anywhere", "allOf: [" + obj + ", {properties: {a: {}}}]\n", false},
		{"a bound beside an entry's own $ref, the rest of that entry", "allOf: [{$ref: '#/components/schemas/Obj', minLength: 1}]\n", true},
		{"a value bound on the schema", "allOf: [" + obj + "]\nminLength: 1\n", true},
		{"a collection bound on the schema", "allOf: [" + obj + "]\nmaxItems: 2\n", true},
		{"a value bound on the first entry", "allOf: [{minimum: 1}, " + obj + "]\n", true},
		{"a value bound on a later entry", "allOf: [" + obj + ", {properties: {a: {}}}, {minLength: 1}]\n", true},
		{"a collection bound on a later entry", "allOf: [" + obj + ", {properties: {a: {}}}, {uniqueItems: true}]\n", true},
		{"a bound on an entry of a nested allOf", "allOf: [" + obj + ", {allOf: [{pattern: '^a'}]}]\n", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s := openapitest.SchemaFromYAML(t.Context(), t, tc.body).GetSchema()
			assert.Equal(t, tc.want, writesBound(lowerer.Ctx, StandaloneSchema(s)))
		})
	}
}

// TestBoundKinds_ReadsEachShapeWhole pins the kinds a bound beside each of
// budgetShapes is read by, each a thousand wide or long or 300 deep: a read of any size is
// decided, a null branch adds no kind, a chain of dynamic references is read
// to the schema it expands to, and a read meeting a cycle of $refs or a $ref
// landing on no schema leaves the position's own type to decide.
func TestBoundKinds_ReadsEachShapeWhole(t *testing.T) {
	t.Parallel()
	serviceContext := servicetest.Context(t, openapitest.ComponentSpecVer("3.1.0", budgetShapes(1000)), service.Policy{})
	for name, want := range map[string]kindSet{
		"AllOf": kindString, "AnyOf": kindString, "Nulls": kindString, "Trues": anyInstance, "D0": kindString,
		"Deep": kindString, "Dangling": anyInstance, "Mixed": anyInstance, "All": anyInstance,
	} {
		admission := admissionOf(serviceContext.Ctx, heldAt(serviceContext.Ctx, jsontext.Pointer("/components/schemas/"+name)))
		cut := name == "Dangling" || name == "Mixed" || name == "All"
		assert.Equal(t, cut, admission.cut, name)
		if !cut {
			assert.Equal(t, want, admission.kinds, name)
		}
	}
}

// budgetShapes declares the shapes TestBoundKinds_ReadsEachShapeWhole reads,
// each n wide or long, or deep to n levels or 300, which the load reads: entries and branches of each kind, null branches of
// each spelling, a chain of dynamic references, an inline allOf in an anyOf in
// an allOf whose levels each write a union too, and all of them at one
// position beside a $ref and a union of its own.
func budgetShapes(n int) string {
	kinds := []string{"{$ref: '#/components/schemas/Str'}", "{type: string}", "{description: d}", "true",
		"{$ref: '#/components/schemas/Nowhere'}", "{$ref: '#/components/schemas/Cyc'}"}
	nullKinds := []string{"{type: 'null'}", "{type: ['null']}", "{enum: [null]}", "{const: null}"}
	var mixed, nulls, chain strings.Builder
	for i := range n {
		mixed.WriteString(kinds[i%len(kinds)] + ", ")
		nulls.WriteString(nullKinds[i%len(nullKinds)] + ", ")
		fmt.Fprintf(&chain, "    D%d: {$dynamicAnchor: d%d, $dynamicRef: '#d%d'}\n", i, i, i+1)
	}
	deep := "{type: string}"
	for i := range min(n, 300) {
		if i%2 == 0 {
			deep = "{allOf: [" + deep + "], anyOf: [{type: string}]}"
		} else {
			deep = "{anyOf: [" + deep + ", {type: string}]}"
		}
	}
	refs := strings.Repeat("{$ref: '#/components/schemas/Str'}, ", n)
	return "    Str: {type: string}\n    Cyc: {$ref: '#/components/schemas/Cyc2'}\n    Cyc2: {$ref: '#/components/schemas/Cyc'}\n" +
		"    AllOf: {allOf: [" + refs + "]}\n    AnyOf: {anyOf: [" + refs + "]}\n" +
		"    Nulls: {anyOf: [{$ref: '#/components/schemas/Str'}, " + nulls.String() + "]}\n" +
		"    Trues: {allOf: [" + strings.Repeat("true, ", n) + "]}\n" +
		"    Dangling: {anyOf: [" + strings.Repeat("{$ref: '#/components/schemas/Nowhere'}, ", n) + "]}\n" +
		"    Mixed: {allOf: [" + mixed.String() + "]}\n" + chain.String() +
		fmt.Sprintf("    D%d: {$dynamicAnchor: d%d, type: string}\n", n, n) + "    Deep: " + deep + "\n" +
		"    All: {allOf: [{$ref: '#/components/schemas/Deep'}, {$ref: '#/components/schemas/Mixed'}, " +
		"{$ref: '#/components/schemas/D0'}], $ref: '#/components/schemas/Str', anyOf: [{$ref: '#/components/schemas/Nulls'}]}\n"
}

// TestBoundKinds_ACycleLeavesTheOwnTypeToDecide pins the fallback of a read cut
// by a cycle of conjunct edges, of dynamic references or of $refs, and by a
// $ref landing on no schema: a bound beside it is read by the position's own
// type, every bound where it writes none.
func TestBoundKinds_ACycleLeavesTheOwnTypeToDecide(t *testing.T) {
	t.Parallel()
	serviceContext := servicetest.Context(t, openapitest.ComponentSpecVer("3.1.0", `    A: {$dynamicAnchor: a, $dynamicRef: '#b', minimum: 1}
    B: {$dynamicAnchor: b, $dynamicRef: '#a'}
    R: {allOf: [{$ref: '#/components/schemas/R'}], type: integer, minLength: 1}
    Dangling: {allOf: [{$ref: '#/components/schemas/Nowhere'}], minLength: 1}
`), service.Policy{})
	for name, want := range map[string]kindSet{"A": anyInstance, "R": kindInteger, "Dangling": anyInstance} {
		st := heldAt(serviceContext.Ctx, jsontext.Pointer("/components/schemas/"+name))
		require.True(t, admissionOf(serviceContext.Ctx, st).cut, name)
		assert.Equal(t, want, boundKinds(serviceContext.Ctx, st.Schema), name)
	}
}
