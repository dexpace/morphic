// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema_test

import (
	"encoding/json/jsontext"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// d4 is the `$schema` of JSON Schema draft 4, which writes a tuple as a list
// of items, its rest as additionalItems, and spells `$id` as `id`.
const d4 = "$schema: 'http://json-schema.org/draft-04/schema#'"

// TestLower_AListOfItemsInADraftIsATuple pins the lowering of a list of items
// in a draft that reads it as a tuple, and in one that does not: its entries
// are the Tuple's Elems and additionalItems its Rest, none for `false`, the top
// type absent, and the schema it holds otherwise; elsewhere `items` reads as
// absent, a List of the top type, or beside prefixItems a Rest of it, with no
// false schema of the parser's making.
func TestLower_AListOfItemsInADraftIsATuple(t *testing.T) {
	t.Parallel()
	const list = "type: array, items: [{type: string}, {type: integer}]"
	doc, diags := parseFull(t, openapitest.ComponentSpec(
		"    C: {"+d4+", "+list+", additionalItems: false}\n"+
			"    O: {"+d4+", "+list+"}\n"+
			"    R: {"+d4+", "+list+", additionalItems: {type: boolean}}\n"+
			"    L: {"+list+"}\n"+
			"    P: {type: array, prefixItems: [{type: string}], items: [{type: integer}]}\n"))
	assert.Equal(t, []jsontext.Pointer{"/components/schemas/L/items", "/components/schemas/P/items"},
		openapitest.PointersOf(diags, diag.InvalidItemsList))
	assert.False(t, openapitest.HasDiag(diags, diag.FalseSchema))
	rests := map[string]*ir.TypeRef{}
	for _, name := range []string{"C", "O", "R", "P"} {
		tp, ok := doc.Types[componentID(name)].(*ir.Tuple)
		require.True(t, ok, "%s lowers to a tuple", name)
		rests[name] = tp.Rest
	}
	assert.Len(t, doc.Types[componentID("C")].(*ir.Tuple).Elems, 2)
	assert.Nil(t, rests["C"])
	require.NotNil(t, rests["O"])
	assert.Equal(t, ir.TypeID("t/prim/any"), rests["O"].Target)
	require.NotNil(t, rests["R"])
	assert.Equal(t, ir.TypeID("t/prim/bool"), rests["R"].Target)
	require.NotNil(t, rests["P"])
	assert.Equal(t, ir.TypeID("t/prim/any"), rests["P"].Target, "a list beside prefixItems reads as no items")
	l, ok := doc.Types[componentID("L")].(*ir.List)
	require.True(t, ok)
	assert.Equal(t, ir.TypeID("t/prim/any"), l.Elem.Target)
}

// TestPreserveUnknownKeywords_ReadsWhatADraftSpellsOtherwise pins what the
// census keeps of a draft 4 schema: additionalItems nothing where the tuple
// holds it as its rest, and verbatim where the position lowered to no tuple;
// `dependencies` as a validation-only keyword; `id` as `$id` is kept; and
// `definitions` nothing, as `$defs` is not.
func TestPreserveUnknownKeywords_ReadsWhatADraftSpellsOtherwise(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.ComponentSpec(
		"    T: {"+d4+", type: array, items: [{type: string}], additionalItems: false}\n"+
			"    U: {"+d4+", items: [{type: string}], additionalItems: false}\n"+
			"    D: {"+d4+", type: object, id: 'https://example.com/d', dependencies: {a: [b]}, definitions: {X: {}}}\n"))
	openapitest.RequireNoErrorDiags(t, diags)
	keys := func(name string) []string {
		um := doc.Types[componentID(name)].Common().Unmodeled
		out := make([]string, 0, len(um))
		for key := range um {
			out = append(out, key)
		}
		return out
	}
	assert.NotContains(t, keys("T"), "openapi:additionalItems", "the tuple holds its rest")
	assert.Contains(t, keys("U"), "openapi:additionalItems", "no tuple holds it")
	d := doc.Types[componentID("D")].Common().Unmodeled
	assert.Equal(t, ir.ReasonValidationOnly, d["openapi:dependencies"].Reason)
	assert.Equal(t, ir.ReasonOutOfScope, d["openapi:id"].Reason)
	assert.NotContains(t, keys("D"), "openapi:definitions")
	assert.True(t, openapitest.HasDiagCodeAt(diags, diag.DegradedConstruct, "/components/schemas/D/id"))
}

// TestRefSite_AKeywordBesideADraftsRefIsWarnedOf pins the keywords beside the
// $ref of a schema read in a draft that ignores them: each draws a
// later-version warning at its key, bar `$schema` and an extension, and is
// applied as 2020-12 applies it.
func TestRefSite_AKeywordBesideADraftsRefIsWarnedOf(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.ComponentSpec("    B: {type: string}\n"+
		"    V: {"+d4+", $ref: '#/components/schemas/B', description: d, x-note: n}\n"))
	openapitest.RequireNoErrorDiags(t, diags)
	assert.Equal(t, []jsontext.Pointer{"/components/schemas/V/description"}, openapitest.PointersOf(diags, diag.LaterVersionConstruct))
	assert.Equal(t, "d", doc.Types[componentID("V")].Common().Docs.Description)
}

// TestUnion_ABranchWritingAListOfItemsRestatesNothing pins that a union branch
// writing `items` as a list, a tuple in a draft, restates no element the body
// declares, so the union is kept as a constraint beside the body rather than
// read as validation the body already says.
func TestUnion_ABranchWritingAListOfItemsRestatesNothing(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.ComponentSpec("    U:\n      "+d4+"\n      type: object\n"+
		"      properties: {p: {type: array, items: {type: string}}}\n"+
		"      oneOf: [{properties: {p: {items: [{type: integer}]}}}, {properties: {p: {maxItems: 1}}}]\n"))
	openapitest.RequireNoErrorDiags(t, diags)
	u := doc.Types[componentID("U")].Common().Unmodeled
	assert.Equal(t, ir.ReasonDegradedLowering, u["openapi:oneOf"].Reason)
}

// TestAllOf_ABranchUnderADraftsIDFoldsNothing pins that draft 4's `id` gives an
// inline allOf branch its base as `$id` does: `n.json` names A under the
// branch's id and nothing at the position, so its `not` and description stay
// with the branch rather than fold into the position.
func TestAllOf_ABranchUnderADraftsIDFoldsNothing(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.ComponentSpec("    A: {"+d4+", id: 'https://ex.test/a/n.json', type: string}\n"+
		"    S:\n      "+d4+"\n      allOf:\n"+
		"        - {type: object, properties: {a: {type: string}}}\n"+
		"        - {id: 'https://ex.test/a/x.json', not: {$ref: 'n.json'}, description: d}\n"))
	openapitest.RequireNoErrorDiags(t, diags)
	s := doc.Types[componentID("S")].Common()
	assert.Contains(t, s.Unmodeled, "openapi:allOf/1", "the branch is kept whole")
	assert.NotContains(t, s.Unmodeled, "openapi:not", "its not folds nowhere")
	assert.Empty(t, s.Docs.Description)
}
