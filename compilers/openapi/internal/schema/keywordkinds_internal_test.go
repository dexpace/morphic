// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"encoding/json/jsontext"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering/loweringtest"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/provenance"
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
		read, _, _ := annotation.Constraints(s, boundsFor(kinds), false, "/p", provenance.Doc{})
		assert.NotNil(t, read, keyword)
		unread, _, _ := annotation.Constraints(s, boundsFor(anyInstance&^kinds), false, "/p", provenance.Doc{})
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
		assert.Equal(t, want, declaredInstanceTypes(s), body)
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

// TestWritesBound_FindsABoundOnTheSchemaOrAnInlineBranch pins when a schema's
// bounds are read at all: a value or collection bound on the schema, on an inline
// branch or on a branch of a nested allOf counts, wherever the branch sits. One
// beside a branch's own $ref is that branch's, and does not.
func TestWritesBound_FindsABoundOnTheSchemaOrAnInlineBranch(t *testing.T) {
	t.Parallel()
	const obj = "{$ref: '#/components/schemas/Obj'}"
	for _, tc := range []struct {
		name, body string
		want       bool
	}{
		{"no bound anywhere", "allOf: [" + obj + ", {properties: {a: {}}}]\n", false},
		{"a bound beside a branch's own $ref", "allOf: [{$ref: '#/components/schemas/Obj', minLength: 1}]\n", false},
		{"a value bound on the schema", "allOf: [" + obj + "]\nminLength: 1\n", true},
		{"a collection bound on the schema", "allOf: [" + obj + "]\nmaxItems: 2\n", true},
		{"a value bound on the first branch", "allOf: [{minimum: 1}, " + obj + "]\n", true},
		{"a value bound on a later branch", "allOf: [" + obj + ", {properties: {a: {}}}, {minLength: 1}]\n", true},
		{"a collection bound on a later branch", "allOf: [" + obj + ", {properties: {a: {}}}, {uniqueItems: true}]\n", true},
		{"a bound on a branch of a nested allOf", "allOf: [" + obj + ", {allOf: [{pattern: '^a'}]}]\n", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s := openapitest.SchemaFromYAML(t.Context(), t, tc.body).GetSchema()
			assert.Equal(t, tc.want, writesBound(s))
		})
	}
}

// TestBoundTypes_ReadsNoSiblingPastItsBudget pins the cost of a read past
// maxBoundTypeReads, by the reads it spends: an allOf or a union wider than the
// budget is read up to the read that exhausts it and no further, as each
// sibling past it would answer anyInstance and change nothing. A union's null
// branches are read at a step each, so a union collapsing onto one branch beside
// many costs no more than the budget either, and so is an entry reading no
// schema, a boolean one or a $ref landing on none.
func TestBoundTypes_ReadsNoSiblingPastItsBudget(t *testing.T) {
	t.Parallel()
	refs := strings.Repeat("{$ref: '#/components/schemas/Str'}, ", 4*maxBoundTypeReads)
	nulls := strings.Repeat("{type: 'null'}, ", 4*maxBoundTypeReads)
	trues := strings.Repeat("true, ", 4*maxBoundTypeReads)
	dangling := strings.Repeat("{$ref: '#/components/schemas/Nowhere'}, ", 4*maxBoundTypeReads)
	c := loweringtest.Context(t, openapitest.ComponentSpecVer("3.1.0",
		"    AllOf: {allOf: ["+refs+"]}\n    AnyOf: {anyOf: ["+refs+"]}\n"+
			"    Nulls: {anyOf: [{$ref: '#/components/schemas/Str'}, "+nulls+"]}\n    Str: {type: string}\n"+
			"    Trues: {allOf: ["+trues+"]}\n    Dangling: {anyOf: ["+dangling+"]}\n"), lowering.Policy{})
	for _, name := range []string{"AllOf", "AnyOf", "Nulls", "Trues", "Dangling"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			pointer := jsontext.Pointer("/components/schemas/" + name)
			s := heldAt(c, pointer).GetSchema()
			require.NotNil(t, s)
			r := boundTypes{steps: budget.New(budget.AtMost(maxBoundTypeReads))}
			assert.Equal(t, anyInstance, r.schema(c, s, pointer))
			assert.Equal(t, maxBoundTypeReads+1, r.steps.Spent())
		})
	}
}

// TestBoundTypes_ADynamicCycleEndsOnTheBudget pins the read of dynamic
// references whose expansions close a cycle: each hop is one schema read at one
// step, so the budget ends the cycle, as it ends one of $refs, and no hop walks
// the chain of expansions again.
func TestBoundTypes_ADynamicCycleEndsOnTheBudget(t *testing.T) {
	t.Parallel()
	c := loweringtest.Context(t, openapitest.ComponentSpecVer("3.1.0",
		"    A: {$dynamicAnchor: a, $dynamicRef: '#b'}\n    B: {$dynamicAnchor: b, $dynamicRef: '#a'}\n"), lowering.Policy{})
	const pointer = jsontext.Pointer("/components/schemas/A")
	s := heldAt(c, pointer).GetSchema()
	require.NotNil(t, s)
	r := boundTypes{steps: budget.New(budget.AtMost(maxBoundTypeReads))}
	assert.Equal(t, anyInstance, r.schema(c, s, pointer))
	assert.Equal(t, maxBoundTypeReads+1, r.steps.Spent())
}

// TestBoundTypes_AReferenceLandingOnNoSchemaExcludesNothing pins the read of a
// $ref whose chain ends on no schema: it admits every kind at the step a read
// of a schema spends, so a bound beside it is read as beside a schema writing
// no type.
func TestBoundTypes_AReferenceLandingOnNoSchemaExcludesNothing(t *testing.T) {
	t.Parallel()
	c := loweringtest.Context(t, openapitest.ComponentSpecVer("3.1.0",
		"    Dangling: {allOf: [{$ref: '#/components/schemas/Nowhere'}]}\n"), lowering.Policy{})
	const pointer = jsontext.Pointer("/components/schemas/Dangling")
	s := heldAt(c, pointer).GetSchema()
	require.NotNil(t, s)
	r := boundTypes{steps: budget.New(budget.AtMost(maxBoundTypeReads))}
	assert.Equal(t, anyInstance, r.schema(c, s, pointer))
	assert.Equal(t, 2, r.steps.Spent())
}
