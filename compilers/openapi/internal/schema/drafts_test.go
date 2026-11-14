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
// holds it as its rest, and where the position lowered to no tuple, verbatim
// beside its list of items as a degraded lowering, as the twin `items` beside
// `prefixItems` is, but beside an `items` that is no list as an unknown
// keyword; `dependencies` as a validation-only keyword; `id` as `$id` is kept;
// and `definitions` nothing, as `$defs` is not.
func TestPreserveUnknownKeywords_ReadsWhatADraftSpellsOtherwise(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.ComponentSpec(
		"    T: {"+d4+", type: array, items: [{type: string}], additionalItems: false}\n"+
			"    U: {"+d4+", items: [{type: string}], additionalItems: false}\n"+
			"    V: {"+d4+", type: array, items: {type: string}, additionalItems: false}\n"+
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
	u := doc.Types[componentID("U")].Common().Unmodeled
	assert.Equal(t, ir.ReasonDegradedLowering, u["openapi:additionalItems"].Reason, "no tuple holds it")
	assert.Equal(t, ir.ReasonDegradedLowering, u["openapi:items"].Reason)
	assert.Equal(t, ir.ReasonOutOfScope, doc.Types[componentID("V")].Common().Unmodeled["openapi:additionalItems"].Reason,
		"it is no tuple's rest beside one schema of items")
	d := doc.Types[componentID("D")].Common().Unmodeled
	assert.Equal(t, ir.ReasonValidationOnly, d["openapi:dependencies"].Reason)
	assert.Equal(t, ir.ReasonOutOfScope, d["openapi:id"].Reason)
	assert.NotContains(t, keys("D"), "openapi:definitions")
	assert.True(t, openapitest.HasDiagCodeAt(diags, diag.DegradedConstruct, "/components/schemas/D/id"))
}

// TestLower_PrefixItemsBesideAListedTupleIsKept pins a draft's schema writing
// its own tuple, a list of items, and 2020-12's prefixItems beside it: the
// list is the tuple, and prefixItems is kept verbatim under degraded_lowering
// with one info at it, as no text makes the two one tuple. Beside one schema
// of items, which the draft reads as no tuple, prefixItems is the tuple.
func TestLower_PrefixItemsBesideAListedTupleIsKept(t *testing.T) {
	t.Parallel()
	const d7 = "$schema: 'http://json-schema.org/draft-07/schema#'"
	doc, diags := parseFull(t, openapitest.ComponentSpec(
		"    Both: {"+d7+", type: array, items: [{type: string}], prefixItems: [{type: integer}]}\n"+
			"    One: {"+d7+", type: array, items: {}, prefixItems: [{type: integer}]}\n"))
	both, ok := doc.Types[componentID("Both")].(*ir.Tuple)
	require.True(t, ok)
	require.Len(t, both.Elems, 1)
	assert.Equal(t, ir.TypeID("t/prim/string"), both.Elems[0].Target, "the list is the tuple")
	kept, ok := both.Unmodeled["openapi:prefixItems"]
	require.True(t, ok)
	assert.Equal(t, ir.ReasonDegradedLowering, kept.Reason)
	assert.JSONEq(t, `[{"type": "integer"}]`, string(kept.Value))
	var at []jsontext.Pointer
	for _, d := range diags {
		if d.Code == diag.DegradedConstruct && d.Provenance.Pointer == "/components/schemas/Both/prefixItems" {
			at = append(at, d.Provenance.Pointer)
			assert.Equal(t, "prefixItems is kept verbatim: this schema's dialect reads its items list as the tuple", d.Message)
		}
	}
	assert.Len(t, at, 1)
	one, ok := doc.Types[componentID("One")].(*ir.Tuple)
	require.True(t, ok)
	require.Len(t, one.Elems, 1)
	assert.Equal(t, ir.TypeID("t/prim/integer"), one.Elems[0].Target)
	assert.NotContains(t, one.Unmodeled, "openapi:prefixItems")
}

// TestUnion_ABranchWritingAListOfItemsRestatesNothing pins that a union branch
// writing `items` as a list, a tuple in a draft, restates no element the body
// declares, so the union is distributed across the body rather than read as
// validation the body already says.
func TestUnion_ABranchWritingAListOfItemsRestatesNothing(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.ComponentSpec("    U:\n      "+d4+"\n      type: object\n"+
		"      properties: {p: {type: array, items: {type: string}}}\n"+
		"      oneOf: [{properties: {p: {items: [{type: integer}]}}}, {properties: {p: {maxItems: 1}}}]\n"))
	openapitest.RequireNoErrorDiags(t, diags)
	assert.Equal(t, readDistributed, unionReading(doc, "U"))
}

// TestUnion_AListOfItemsBesideTheBranchesIsNotSaid pins that a body writing
// `items` as a list, a tuple in a draft, is said by no branch writing one
// element schema, so the union keeps the body rather than lowering alone and
// dropping the tuple (docs/compilers/openapi.md#union-beside-structure).
func TestUnion_AListOfItemsBesideTheBranchesIsNotSaid(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.ComponentSpec("    U:\n      "+d4+"\n      type: array\n"+
		"      items: [{type: string}, {type: integer}]\n"+
		"      anyOf: [{type: array, items: {type: string, format: uuid}}, {type: array, items: {type: string, format: date}}]\n"))
	openapitest.RequireNoErrorDiags(t, diags)
	assert.Equal(t, string(ir.ReasonDegradedLowering), unionReading(doc, "U"))
	_, tuple := typeByName(doc, "U").(*ir.Tuple)
	assert.True(t, tuple, "the body keeps its tuple; got %T", typeByName(doc, "U"))
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
