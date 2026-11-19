// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"encoding/json/jsontext"
	"fmt"
	"regexp"
	"slices"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// inlinePrincipal is a schema an allOf entry writes that the position lowers
// from, beside another entry writing other, and the kind of the node it builds.
type inlinePrincipal struct {
	name, keywords, other string
	kind                  ir.TypeKind
	only31                bool // it writes a keyword 3.0 has no spelling of
}

// inlinePrincipals are the principals that build a node: a list, a tuple, a
// type list, a union, a hoisted scalar, a list the kind-inference policy reads,
// a union of null alone, a union of value sets, and a map, which composes and
// stood at its position before; each bound beside it where it takes one.
var inlinePrincipals = []inlinePrincipal{
	{name: "list", keywords: "type: array, items: {type: string}", other: "description: d", kind: ir.KindList},
	{name: "list bounded apart", keywords: "type: array, items: {type: string}", other: "minItems: 1", kind: ir.KindList},
	{
		name: "tuple", keywords: "type: array, prefixItems: [{type: string}, {type: integer}], items: false",
		other: "description: d", kind: ir.KindTuple, only31: true,
	},
	{name: "type list", keywords: "type: [string, integer], minLength: 2", other: "description: d", kind: ir.KindUnion, only31: true},
	{name: "type list bounded apart", keywords: "type: [string, integer]", other: "minLength: 2", kind: ir.KindUnion, only31: true},
	{name: "union", keywords: "oneOf: [{$ref: '#/components/schemas/S'}, {type: integer}]", other: "description: d", kind: ir.KindUnion},
	{name: "union bounded apart", keywords: "oneOf: [{$ref: '#/components/schemas/S'}]", other: "type: string, minLength: 1", kind: ir.KindUnion},
	{name: "scalar", keywords: "type: string, format: byte, maxLength: 4", other: "description: d", kind: ir.KindScalar},
	{name: "inferred list", keywords: "items: {type: string}", other: "description: d", kind: ir.KindList},
	{name: "null alone", keywords: "oneOf: [{type: 'null'}]", other: "description: d", kind: ir.KindEnum, only31: true},
	{name: "union of value sets", keywords: "anyOf: [{enum: [a, b]}, {enum: [b, c]}]", other: "description: d", kind: ir.KindUnion},
	{name: "map", keywords: "type: object, additionalProperties: {type: string}", other: "description: d", kind: ir.KindMap},
}

// principalSpellings writes the principal inlinePrincipalValue beside other in the four ways a
// conjunction spells it, the first the keywords beside each other, each beside
// an entry ref, a $ref to T, where one is given.
func principalSpellings(inlinePrincipalValue inlinePrincipal, ref string) map[string]string {
	k, o := "{"+inlinePrincipalValue.keywords+"}", "{"+inlinePrincipalValue.other+"}"
	refs := ""
	if ref != "" {
		refs = ref + ", "
	}
	beside := "{" + inlinePrincipalValue.other + ", " + inlinePrincipalValue.keywords + "}"
	if ref != "" {
		beside = "{allOf: [" + ref + "], " + inlinePrincipalValue.other + ", " + inlinePrincipalValue.keywords + "}"
	}
	return map[string]string{
		"beside":      beside,
		"entry last":  "{allOf: [" + refs + o + ", " + k + "]}",
		"entry first": "{allOf: [" + k + ", " + refs + o + "]}",
		"entry alone": "{allOf: [" + refs + k + "], " + inlinePrincipalValue.other + "}",
	}
}

// principalVersions are the versions a principal is written in.
func principalVersions(inlinePrincipalValue inlinePrincipal) []string {
	if inlinePrincipalValue.only31 {
		return []string{"3.1.0", "3.2.0"}
	}
	return []string{"3.0.3", "3.1.0", "3.2.0"}
}

// standingWritten is a document as the cases compare it: its tree without
// sources and findings, every pointer an entry index stands on left out
// (entryIndexless), and the findings as severity, code and pointer, sorted.
type standingWritten struct {
	doc      string
	findings []string
}

// writtenStanding compiles spec and reads it as standingWritten.
func writtenStanding(t *testing.T, spec string) (standingWritten, *ir.Document) {
	t.Helper()
	doc, diags := compileUnrefused(t, spec)
	tree, ok := documentTree(t, doc).(map[string]any)
	require.True(t, ok, "a document encodes as an object")
	delete(tree, "sources")
	delete(tree, "diagnostics")
	got := standingWritten{doc: fmt.Sprint(entryIndexless(tree))}
	for _, diagnostic := range diags {
		got.findings = append(got.findings, fmt.Sprintf("%s %s %s", diagnostic.Severity, diagnostic.Code, entryIndexless(string(diagnostic.Provenance.Pointer))))
	}
	slices.Sort(got.findings)
	return got, doc
}

// atAnEntry matches a type ID standing at an allOf entry's own pointer.
var atAnEntry = regexp.MustCompile(`/allOf/[0-9]+$`)

// TestInlinePrincipal_TheNodeStandsAtThePositionInEverySpelling holds every
// principal that builds a node, at every site, in each version that spells it,
// with and without a $ref entry it beats, written on an entry first, last or
// alone to the document its keywords beside each other compile to, up to the
// entries' indices in its children's pointers and in its findings: the node at
// the position's ID, of the principal's kind, and no type at an entry.
func TestInlinePrincipal_TheNodeStandsAtThePositionInEverySpelling(t *testing.T) {
	t.Parallel()
	for _, inlinePrincipal := range inlinePrincipals {
		t.Run(inlinePrincipal.name, func(t *testing.T) {
			t.Parallel()
			for _, version := range principalVersions(inlinePrincipal) {
				for _, ref := range []string{"", "{" + refTo("T") + "}"} {
					spellings := principalSpellings(inlinePrincipal, ref)
					for _, site := range conjSites {
						requireStandsAtPosition(t, inlinePrincipal, version, site, spellings)
					}
				}
			}
		})
	}
}

// requireStandsAtPosition is one site and version of
// TestInlinePrincipal_TheNodeStandsAtThePositionInEverySpelling.
func requireStandsAtPosition(t *testing.T, inlinePrincipalValue inlinePrincipal, version string, site conjSite, spellings map[string]string) {
	t.Helper()
	others := []string{"S: {type: string}", "T: {}"}
	want, _ := writtenStanding(t, site.write(version, spellings["beside"], others...))
	for name, schema := range spellings {
		got, doc := writtenStanding(t, site.write(version, schema, others...))
		at := fmt.Sprintf("%s at the %s in %s, %s", inlinePrincipalValue.name, site.name, version, schema)
		node := kindNode(t, doc, entryPositionID(jsontext.Pointer(site.pointer)))
		assert.Equal(t, inlinePrincipalValue.kind, node.Kind(), "%s: the node at the position", at)
		for id := range doc.Types {
			assert.False(t, atAnEntry.MatchString(string(id)), "%s: %s stands at an entry", at, id)
		}
		if name == "beside" {
			continue
		}
		assert.Empty(t, cmp.Diff(want.doc, got.doc), "%s: the document", at)
		assert.Empty(t, cmp.Diff(want.findings, got.findings), "%s: the findings", at)
	}
}

// boundedPrincipal is an allOf of a principal and an entry bounding it, and the
// bound the node the position owns holds.
type boundedPrincipal struct {
	name, principal, bound string
	check                  func(t *testing.T, doc *ir.Document, node ir.TypeDef)
}

// boundedPrincipals are #2053's union bounded by a minLength, a list by a
// minItems, and a type list by a minLength, which its string variant holds.
var boundedPrincipals = []boundedPrincipal{
	{name: "union", principal: "{oneOf: [{$ref: '#/components/schemas/S'}]}", bound: "{type: string, minLength: 1}",
		check: func(t *testing.T, _ *ir.Document, node ir.TypeDef) {
			t.Helper()
			union, ok := node.(*ir.Union)
			require.True(t, ok, "a union")
			require.NotNil(t, union.Constraints)
			require.NotNil(t, union.Constraints.MinLength)
			assert.EqualValues(t, 1, *union.Constraints.MinLength)
		}},
	{name: "list", principal: "{type: array, items: {type: string}}", bound: "{minItems: 1}",
		check: func(t *testing.T, _ *ir.Document, node ir.TypeDef) {
			t.Helper()
			list, ok := node.(*ir.List)
			require.True(t, ok, "a list")
			require.NotNil(t, list.Constraints)
			require.NotNil(t, list.Constraints.MinItems)
			assert.EqualValues(t, 1, *list.Constraints.MinItems)
		}},
	{name: "type list", principal: "{type: [string, integer]}", bound: "{minLength: 2}",
		check: func(t *testing.T, doc *ir.Document, node ir.TypeDef) {
			t.Helper()
			union, ok := node.(*ir.Union)
			require.True(t, ok, "a union")
			assert.Nil(t, union.Constraints, "the union holds no bound its variants hold")
			require.Len(t, union.Variants, 2)
			str, ok := kindNode(t, doc, union.Variants[0].Type.Target).(*ir.Scalar)
			require.True(t, ok, "the string variant is a scalar of its own")
			require.NotNil(t, str.Constraints)
			require.NotNil(t, str.Constraints.MinLength)
			assert.EqualValues(t, 2, *str.Constraints.MinLength)
		}},
}

// TestInlinePrincipal_TheNodeHoldsTheBoundsOfTheConjunction holds the bound an
// entry writes beside an inline principal to the node the position owns, at a
// component, a property and items, in either order of the entries: no
// carrier, alias or Unmodeled holds it.
func TestInlinePrincipal_TheNodeHoldsTheBoundsOfTheConjunction(t *testing.T) {
	t.Parallel()
	for _, b := range boundedPrincipals {
		for _, swapped := range []bool{false, true} {
			entries := "[" + b.principal + ", " + b.bound + "]"
			if swapped {
				entries = "[" + b.bound + ", " + b.principal + "]"
			}
			spec := admissionSpec("3.1.0", "", "S: {type: string}",
				"Comp: {allOf: "+entries+"}",
				"Prop: {type: object, properties: {p: {allOf: "+entries+"}}}",
				"Items: {type: array, items: {allOf: "+entries+"}}")
			doc, diags := compileUnrefused(t, spec)
			requireErrorsAndWarnings(t, diags, nil)
			for _, id := range []ir.TypeID{componentID("Comp"), "type/anonymous/components/schemas/Prop/properties/p", "type/anonymous/components/schemas/Items/items"} {
				node := kindNode(t, doc, id)
				b.check(t, doc, node)
				assert.Empty(t, keptOn(node), "%s, %s: nothing kept", b.name, id)
			}
			prop := requireModelOf(t, doc, "Prop").Properties[0]
			assert.Nil(t, prop.Constraints, "%s: the property holds no bound", b.name)
			assert.Empty(t, prop.Unmodeled, "%s: nor keeps one", b.name)
			list, ok := kindNode(t, doc, componentID("Items")).(*ir.List)
			require.True(t, ok)
			assert.Equal(t, ir.TypeID("type/anonymous/components/schemas/Items/items"), list.ElementType.Target, "%s: the element is the node", b.name)
		}
	}
}

// TestInlinePrincipal_AReferenceToTheEntryNamesTheEntryAlone holds a $ref to
// an entry an allOf lowers from to a node at the entry's own ID typed by what
// the entry writes alone, keeping the readOnly it writes as a component does,
// beside the node at the position holding the bounds of the whole
// conjunction, whichever is declared first.
func TestInlinePrincipal_AReferenceToTheEntryNamesTheEntryAlone(t *testing.T) {
	t.Parallel()
	schemas := []string{
		"ArrE: {allOf: [{description: d, minItems: 2}, {type: array, items: {type: string}, maxItems: 9, readOnly: true}]}",
		"R: {$ref: '#/components/schemas/ArrE/allOf/1'}",
	}
	docs := map[string]string{}
	inBothOrders(t, schemas, func(t *testing.T, schemas []string) {
		doc, diags := compileUnrefused(t, admissionSpec("3.1.0", "", schemas...))
		requireErrorsAndWarnings(t, diags, nil)
		whole, ok := kindNode(t, doc, componentID("ArrE")).(*ir.List)
		require.True(t, ok, "the position is the list")
		require.NotNil(t, whole.Constraints)
		require.NotNil(t, whole.Constraints.MinItems)
		require.NotNil(t, whole.Constraints.MaxItems)
		assert.EqualValues(t, 2, *whole.Constraints.MinItems)
		assert.EqualValues(t, 9, *whole.Constraints.MaxItems)
		assert.Equal(t, "d", whole.Docs.Description, "the position's description")

		entry, ok := kindNode(t, doc, "type/anonymous/components/schemas/ArrE/allOf/1").(*ir.List)
		require.True(t, ok, "the entry a $ref names is a list of its own")
		require.NotNil(t, entry.Constraints)
		assert.Nil(t, entry.Constraints.MinItems, "the entry alone writes no minItems")
		require.NotNil(t, entry.Constraints.MaxItems)
		assert.EqualValues(t, 9, *entry.Constraints.MaxItems)
		assert.Empty(t, entry.Docs.Description, "nor the description beside it")
		assert.Contains(t, keptOn(entry), "openapi:readOnly", "a use-bound keyword it writes is kept on it")
		assert.Equal(t, entry.ElementType.Target, whole.ElementType.Target, "one element, at the entry's items")
		docs[t.Name()] = withoutVersion(t, doc)
	})
	declared, reversed := docs[t.Name()+"/declared"], docs[t.Name()+"/reversed"]
	require.NotEmpty(t, declared)
	assert.Empty(t, cmp.Diff(declared, reversed), "either order of declaration compiles one document")
}

// TestInlinePrincipal_ChildrenKeepThePointerTheyAreWrittenAt holds what an
// inline principal holds beneath it to the pointer it is written at: a list's
// element at the entry's items, the position's own items kept and no element,
// and a union's branch at the entry's oneOf. A type list's variant, written at
// no pointer, stands under the position's, as its flat spelling's does.
func TestInlinePrincipal_ChildrenKeepThePointerTheyAreWrittenAt(t *testing.T) {
	t.Parallel()
	spec := admissionSpec("3.1.0", "",
		"L: {allOf: [{type: array, items: {type: object, properties: {a: {}}}}], items: {type: object, properties: {b: {}}}}",
		"Tl: {allOf: [{description: d}, {type: [object, string], properties: {a: {}}}]}",
		"U: {allOf: [{description: d}, {oneOf: [{type: object, properties: {a: {}}}, {type: integer}]}]}")
	doc, _ := compileUnrefused(t, spec)
	const at = "type/anonymous/components/schemas/"
	list, ok := kindNode(t, doc, componentID("L")).(*ir.List)
	require.True(t, ok, "L is the list")
	assert.Equal(t, ir.TypeID(at+"L/allOf/0/items"), list.ElementType.Target, "the element where it is written")
	assert.Contains(t, keptOn(list), "openapi:items", "the position's own items is kept")
	_, own := doc.Types[at+"L/items"]
	assert.False(t, own, "and builds no node")

	tl, ok := kindNode(t, doc, componentID("Tl")).(*ir.Union)
	require.True(t, ok, "Tl is the union of its types")
	require.Len(t, tl.Variants, 2)
	assert.Equal(t, ir.TypeID(at+"Tl/type/0"), tl.Variants[0].Type.Target)

	union, ok := kindNode(t, doc, componentID("U")).(*ir.Union)
	require.True(t, ok, "U is the union")
	require.Len(t, union.Variants, 2)
	assert.Equal(t, ir.TypeID(at+"U/allOf/1/oneOf/0"), union.Variants[0].Type.Target)
}

// TestInlinePrincipal_AWrapperHoldsEachBoundOnce holds a principal written as
// a wrapper of an entry bounding it to the one list its flat spelling is, the
// bound held once, at a component and at a property, whose carrier holds none.
func TestInlinePrincipal_AWrapperHoldsEachBoundOnce(t *testing.T) {
	t.Parallel()
	wrapped := "{allOf: [{allOf: [{minItems: 1}], type: array, items: {type: string}}]}"
	flat := "{allOf: [{minItems: 1}], type: array, items: {type: string}}"
	for _, schema := range []string{wrapped, flat} {
		doc, diags := compileUnrefused(t, admissionSpec("3.1.0", "",
			"Use: "+schema, "H: {type: object, properties: {p: "+schema+"}}"))
		requireErrorsAndWarnings(t, diags, nil)
		for _, id := range []ir.TypeID{componentID("Use"), "type/anonymous/components/schemas/H/properties/p"} {
			list, ok := kindNode(t, doc, id).(*ir.List)
			require.True(t, ok, "%s: %s is the list", schema, id)
			require.NotNil(t, list.Constraints)
			require.NotNil(t, list.Constraints.MinItems)
			assert.EqualValues(t, 1, *list.Constraints.MinItems)
		}
		property := requireModelOf(t, doc, "H").Properties[0]
		assert.Nil(t, property.Constraints, "%s: the carrier holds no bound", schema)
		for id := range doc.Types {
			assert.NotContains(t, string(id), "/allOf/", "%s: %s stands at an entry", schema, id)
		}
	}
}
