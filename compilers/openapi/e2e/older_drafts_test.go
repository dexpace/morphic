// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"encoding/json/jsontext"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// The `$schema` of each JSON Schema draft before 2020-12 the table has rows
// for, and of 2020-12 itself.
const (
	draft06   = "'http://json-schema.org/draft-06/schema#'"
	draft07   = "'http://json-schema.org/draft-07/schema#'"
	draft2019 = "'https://json-schema.org/draft/2019-09/schema'"
	draft2020 = "'https://json-schema.org/draft/2020-12/schema'"
)

// TestOlderDrafts_AListOfItemsIsATuple pins a list of items in each draft that
// spells a tuple so, at 3.1 and 3.2: a Tuple of its entries, an entry's $ref
// bound, whose rest is additionalItems, none for `false` and the top type for
// an absent one. In 2020-12, and in a 3.0 document, a list is no schema: an
// openapi/invalid-items-list error at it, and a List of the top type.
func TestOlderDrafts_AListOfItemsIsATuple(t *testing.T) {
	t.Parallel()
	const list = "type: array, items: [{type: string}, {$ref: '#/components/schemas/B'}]"
	for _, version := range []string{"3.1.0", "3.2.0"} {
		for _, draft := range []string{draft04, draft06, draft07, draft2019} {
			t.Run(version+" "+draft, func(t *testing.T) {
				t.Parallel()
				doc, diags := parseFull(t, openapitest.ComponentSpecVer(version, "    B: {type: integer}\n"+
					"    C: {$schema: "+draft+", "+list+", additionalItems: false}\n"+
					"    O: {$schema: "+draft+", "+list+"}\n"))
				openapitest.RequireNoErrorDiags(t, diags)
				closed, ok := doc.Types[componentID("C")].(*ir.Tuple)
				require.True(t, ok, "a list of items is a tuple")
				require.Len(t, closed.Elems, 2)
				assert.Equal(t, componentID("B"), closed.Elems[1].Target)
				assert.Nil(t, closed.Rest, "additionalItems: false admits nothing past them")
				open, ok := doc.Types[componentID("O")].(*ir.Tuple)
				require.True(t, ok)
				require.NotNil(t, open.Rest, "an absent additionalItems admits anything past them")
				assert.Equal(t, ir.TypeID("t/prim/any"), open.Rest.Target)
			})
		}
	}
	for _, tc := range []struct{ name, src string }{
		{"2020-12", openapitest.ComponentSpec("    L: {$schema: " + draft2020 + ", " + list + "}\n    B: {}\n")},
		{"3.1's own", openapitest.ComponentSpec("    L: {" + list + "}\n    B: {}\n")},
		{"3.0", openapitest.ComponentSpecVer("3.0.3", "    L: {"+list+"}\n    B: {}\n")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, diags := parseFull(t, tc.src)
			assert.Equal(t, []jsontext.Pointer{"/components/schemas/L/items"}, openapitest.PointersOf(diags, diag.InvalidItemsList))
			assert.False(t, openapitest.HasDiag(diags, diag.FalseSchema), "the list reads as no items, not a false schema")
			l, ok := doc.Types[componentID("L")].(*ir.List)
			require.True(t, ok)
			assert.Equal(t, ir.TypeID("t/prim/any"), l.Elem.Target)
		})
	}
}

// TestOlderDrafts_DependenciesAreValidationOnly pins `dependencies` in each
// draft that defines it, 4, 6 and 7, kept as a validation-only keyword, and in
// 2019-09 and 2020-12, which split it, as an unknown keyword.
func TestOlderDrafts_DependenciesAreValidationOnly(t *testing.T) {
	t.Parallel()
	for draft, want := range map[string]ir.UnmodeledReason{
		draft04: ir.ReasonValidationOnly, draft06: ir.ReasonValidationOnly, draft07: ir.ReasonValidationOnly,
		draft2019: ir.ReasonOutOfScope, draft2020: ir.ReasonOutOfScope,
	} {
		t.Run(draft, func(t *testing.T) {
			t.Parallel()
			doc, diags := parseFull(t, openapitest.ComponentSpec(
				"    D: {$schema: "+draft+", type: object, dependencies: {a: [b]}}\n"))
			openapitest.RequireNoErrorDiags(t, diags)
			d, ok := doc.Types[componentID("D")].(*ir.Model)
			require.True(t, ok)
			assert.Equal(t, want, unmodeledEntry(t, d.Unmodeled, "openapi:dependencies").Reason)
			assert.Equal(t, want == ir.ReasonValidationOnly, openapitest.HasDiag(diags, diag.ValidationOnlyKeyword))
		})
	}
}

// TestOlderDrafts_RefSiblingsAreWarnedOf pins the keywords beside a $ref in
// each draft that ignores them, 4, 6 and 7: each is applied as 2020-12 applies
// it and warned of at its key as a later version's construct, bar the
// `$schema` naming the draft and an extension. 2019-09 and 2020-12 apply them
// and warn of none.
func TestOlderDrafts_RefSiblingsAreWarnedOf(t *testing.T) {
	t.Parallel()
	for draft, warned := range map[string]bool{
		draft04: true, draft06: true, draft07: true, draft2019: false, draft2020: false,
	} {
		t.Run(draft, func(t *testing.T) {
			t.Parallel()
			doc, diags := parseFull(t, openapitest.ComponentSpec("    B: {type: string}\n"+
				"    V: {$schema: "+draft+", $ref: '#/components/schemas/B', description: d, x-note: n}\n"))
			openapitest.RequireNoErrorDiags(t, diags)
			var want []jsontext.Pointer
			if warned {
				want = []jsontext.Pointer{"/components/schemas/V/description"}
			}
			assert.Equal(t, want, openapitest.PointersOf(diags, diag.LaterVersionConstruct))
			v := doc.Types[componentID("V")]
			require.NotNil(t, v)
			assert.Equal(t, "d", v.Common().Docs.Description, "applied in every dialect")
		})
	}
}

// TestOlderDrafts_AnIDNamesAResource pins draft 4's `id`: a resource's base a
// relative $ref resolves against, kept verbatim as `$id` is, and a fragment
// alone declaring a plain name a $ref reads, as draft 7's `$id` does. In
// 2020-12 `id` names nothing, and an `$id` naming a fragment is refused.
func TestOlderDrafts_AnIDNamesAResource(t *testing.T) {
	t.Parallel()
	src := func(draft string) string {
		return openapitest.ComponentSpec("    T: {$schema: " + draft + ", id: 'https://example.com/t', type: object,\n" +
			"      properties: {u: {$ref: 'u'}, leaf: {$ref: '#leaf'}}, definitions: {Leaf: {id: '#leaf', type: string}}}\n" +
			"    U: {$schema: " + draft + ", id: 'https://example.com/u', type: integer}\n")
	}
	doc, diags := parseFull(t, src(draft04))
	openapitest.RequireNoErrorDiags(t, diags)
	tm, ok := doc.Types[componentID("T")].(*ir.Model)
	require.True(t, ok)
	u, ok := propByWire(tm, "u")
	require.True(t, ok)
	assert.Equal(t, componentID("U"), u.Type.Target, "u resolves against the id above it")
	leaf, ok := propByWire(tm, "leaf")
	require.True(t, ok)
	assert.Equal(t, ir.TypeID("t/anon/components/schemas/T/definitions/Leaf"), leaf.Type.Target)
	assert.Equal(t, ir.ReasonOutOfScope, unmodeledEntry(t, tm.Unmodeled, "openapi:id").Reason)
	_, diags = parseFull(t, openapitest.ComponentSpec("    W: {$schema: "+draft04+", $id: 'https://example.com/w', type: string}\n"+
		"    V: {$ref: 'https://example.com/w'}\n"))
	assert.Contains(t, openapitest.DiagMessageAt(t, diags, diag.DegradedConstruct, ir.SeverityInfo, "/components/schemas/W/$id"),
		"is read for nothing", "draft 4 reads no $id")
	assert.True(t, hasErrorRef(diags), "so nothing names W by its $id")

	_, diags = parseFull(t, src(draft2020))
	assert.True(t, hasErrorRef(diags), "2020-12 reads no identifier in id")

	fragment := func(draft, defs string) []ir.Diagnostic {
		_, diags := parseFull(t, openapitest.ComponentSpec("    T: {$schema: "+draft+", type: object,\n"+
			"      properties: {leaf: {$ref: '#leaf'}}, "+defs+": {Leaf: {$id: '#leaf', type: string}}}\n"))
		return diags
	}
	openapitest.RequireNoErrorDiags(t, fragment(draft07, "definitions"))
	assert.Equal(t, []jsontext.Pointer{"/components/schemas/T/$defs/Leaf/$id"},
		openapitest.PointersOf(fragment(draft2020, "$defs"), diag.Validation+"/validation-invalid-format"),
		"draft 7's $id names a fragment alone, which 2020-12's may not")
}

// TestOlderDrafts_AValueNamingNoDialectLeavesItsDocumentsDraft pins a
// `$schema` no absolute URI names, in a document defaulting to draft 4: its
// schema reads draft 4 as though it wrote none, for the identifier the binder
// declares and the tuple it binds as for the lowering, so a $ref naming its
// `id` resolves and an entry of its list of items is bound.
func TestOlderDrafts_AValueNamingNoDialectLeavesItsDocumentsDraft(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, "openapi: 3.1.0\ninfo: {title: T, version: '1'}\njsonSchemaDialect: "+draft04+
		"\npaths: {}\ncomponents:\n  schemas:\n"+
		"    I: {$schema: 'rel/x', id: 'https://example.com/i', type: object}\n"+
		"    R: {$ref: 'https://example.com/i'}\n"+
		"    E: {$schema: 4, type: array, items: [{$ref: '#/components/schemas/S'}], additionalItems: false}\n"+
		"    S: {type: string}\n")
	assert.False(t, hasErrorRef(diags), "R resolves against I's id")
	r, ok := doc.Types[componentID("R")].(*ir.Scalar)
	require.True(t, ok)
	assert.Equal(t, componentID("I"), r.Base.Target)
	e, ok := doc.Types[componentID("E")].(*ir.Tuple)
	require.True(t, ok, "a list of items is a draft 4 tuple")
	require.Len(t, e.Elems, 1)
	assert.Equal(t, componentID("S"), e.Elems[0].Target)
}

// TestOlderDrafts_AnAliasedNodeIsReadInTheDialectOfEachPosition pins a node an
// alias places under a draft 4 root and under a 2020-12 schema: each position
// reads its own dialect, so the keyword beside its $ref is warned of under the
// draft alone, and its dependencies are validation-only there and unknown at
// the other.
func TestOlderDrafts_AnAliasedNodeIsReadInTheDialectOfEachPosition(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.ComponentSpec("    B: {type: string}\n"+
		"    D4:\n      $schema: "+draft04+"\n      type: object\n      properties:\n"+
		"        a: &sib {$ref: '#/components/schemas/B', description: d}\n"+
		"        dep: &dep {type: object, dependencies: {a: [b]}}\n"+
		"    Now:\n      type: object\n      properties: {a: *sib, dep: *dep}\n"))
	openapitest.RequireNoErrorDiags(t, diags)
	assert.Equal(t, []jsontext.Pointer{"/components/schemas/D4/properties/a/description"},
		openapitest.PointersOf(diags, diag.LaterVersionConstruct))
	reason := func(at string) ir.UnmodeledReason {
		td := doc.Types[ir.TypeID("t/anon/components/schemas/"+at)]
		require.NotNil(t, td, at)
		return unmodeledEntry(t, td.Common().Unmodeled, "openapi:dependencies").Reason
	}
	assert.Equal(t, ir.ReasonValidationOnly, reason("D4/properties/dep"))
	assert.Equal(t, ir.ReasonOutOfScope, reason("Now/properties/dep"))
}

// TestOlderDrafts_AnAnchorTakesItsDraftsGrammar pins an anchor's name, read in
// its schema's dialect: the drafts before 2020-12 admit a colon and no leading
// '_', 2020-12 the reverse, so 2019-09's `$anchor: a:b` and draft 7's
// `$id: '#a:b'` each declare a name a $ref resolves to, where 2020-12's
// `$anchor: a:b` and 2019-09's `$anchor: _x` are refused.
func TestOlderDrafts_AnAnchorTakesItsDraftsGrammar(t *testing.T) {
	t.Parallel()
	schema := func(name, draft, defs, decl, anchor string) string {
		head := "    " + name + ":\n"
		if draft != "" {
			head += "      $schema: " + draft + "\n"
		}
		declared := anchor
		if decl == "$id" {
			declared = "#" + anchor
		}
		return head + "      type: object\n      properties: {a: {$ref: '#" + anchor + "'}}\n" +
			"      " + defs + ": {L: {" + decl + ": '" + declared + "', type: string}}\n"
	}
	_, diags := parseFull(t, openapitest.ComponentSpec(schema("A", draft2019, "$defs", "$anchor", "a:b")+
		schema("S", draft07, "definitions", "$id", "e:f")+
		schema("N", "", "$defs", "$anchor", "c:d")+
		schema("U", draft2019, "$defs", "$anchor", "_x")))
	assert.Equal(t, []jsontext.Pointer{"/components/schemas/N/$defs/L/$anchor", "/components/schemas/U/$defs/L/$anchor"},
		openapitest.PointersOf(diags, diag.Validation+"/validation-invalid-format"))
	refs := unresolvedRefs(diags)
	unresolved := make([]jsontext.Pointer, 0, len(refs))
	for _, d := range refs {
		unresolved = append(unresolved, d.Provenance.Pointer)
	}
	assert.ElementsMatch(t, []jsontext.Pointer{"/components/schemas/N/properties/a", "/components/schemas/U/properties/a"}, unresolved)
}
