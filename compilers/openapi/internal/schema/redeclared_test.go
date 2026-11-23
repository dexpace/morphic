// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/schema"
	"github.com/dexpace/morphic/compilers/openapi/internal/statement"
	"github.com/dexpace/morphic/ir"
)

// noted is a list, which builds a node of its own, described DOC.
const noted = "{type: array, items: {type: string}, description: DOC}"

// assertNotesAt holds the list a property declares at pointer to where its
// description is: on its node where onNode, and otherwise on the property the
// model id names holds.
func assertNotesAt(t *testing.T, doc *ir.Document, model ir.TypeID, pointer string, onNode bool, label string) {
	t.Helper()
	node, ok := doc.Types[ir.TypeID("type/anonymous"+pointer)]
	require.True(t, ok, "%s: %s builds a node", label, pointer)
	irModel, ok := doc.Types[model].(*ir.Model)
	require.True(t, ok, "%s: %s is a model", label, model)
	property, ok := openapitest.PropertiesByWire(irModel.Properties)["f"]
	require.True(t, ok, label)
	want := map[bool]string{true: "DOC"}
	assert.Equal(t, want[onNode], node.Common().Docs.Description, "%s: the node at %s", label, pointer)
	assert.Equal(t, want[!onNode], property.Docs.Description, "%s: the property", label)
}

// TestDeclaredAgain_IsReadOfTheModelComposingTheMember pins where a property's
// notes stand by what the model composing it writes: on its node where that
// model declares the name again itself, in its own properties, an inline allOf
// entry, one nested in another or the keywords beside an entry's $ref; and on
// the property where the other declaration is another model's, a schema's the
// property's own type lists, or that of a conjunct an allOf refining lowers
// none of (docs/compilers/openapi.md#allof-redeclared-property).
func TestDeclaredAgain_IsReadOfTheModelComposingTheMember(t *testing.T) {
	t.Parallel()
	f := "properties: {f: " + noted + "}"
	const plain = "properties: {f: {type: array, items: {type: string}}}"
	for _, tc := range []struct {
		name, model string
		at          string // below M, the property the row reads
		node        ir.TypeID
		onNode      bool
	}{
		{"declared once", "{" + f + "}", "/properties/f", "", false},
		{"declared once in an entry", "{allOf: [{" + f + "}, {properties: {g: " + noted + "}}]}", "/allOf/0/properties/f", "", false},
		{"two entries", "{allOf: [{" + f + "}, {" + plain + "}]}", "/allOf/0/properties/f", "", true},
		{"two entries, the later noted", "{allOf: [{" + plain + "}, {" + f + "}]}", "/allOf/1/properties/f", "", true},
		{"an entry and the model's own", "{allOf: [{" + f + "}], " + plain + "}", "/allOf/0/properties/f", "", true},
		{"the model's own and an entry", "{allOf: [{" + plain + "}], " + f + "}", "/properties/f", "", true},
		{"a nested entry and an entry", "{allOf: [{allOf: [{" + f + "}]}, {" + plain + "}]}", "/allOf/0/allOf/0/properties/f", "", true},
		{"a wrapper's own and its entry", "{allOf: [{allOf: [{" + plain + "}], " + f + "}]}", "/allOf/0/properties/f", "", true},
		{"a wrapper's own and another entry", "{allOf: [{allOf: [{properties: {g: {type: string}}}], " + f + "}, {" + plain + "}]}",
			"/allOf/0/properties/f", "", true},
		{"two entries nested in one", "{allOf: [{allOf: [{" + plain + "}, {" + f + "}]}]}", "/allOf/0/allOf/1/properties/f", "", true},
		{"the keywords beside a $ref and an entry", "{allOf: [{$ref: '#/components/schemas/Plain', " + f + "}, {" + plain + "}]}",
			"/allOf/0/properties/f", "", true},
		{"the model's own beside its $ref and an entry", "{$ref: '#/components/schemas/Plain', " + f + ", allOf: [{" + plain + "}]}",
			"/properties/f", "", true},
		{"an entry and what a $ref entry names", "{allOf: [{" + f + "}, {$ref: '#/components/schemas/Base'}]}", "/allOf/0/properties/f", "", false},
		{"the model's own and what its $ref names", "{$ref: '#/components/schemas/Base', " + f + "}", "/properties/f", "", false},
		{"a member of a member of its name", "{properties: {f: {type: object, " + f + "}}}", "/properties/f/properties/f",
			"type/anonymous/components/schemas/M/properties/f", false},
		{"a member of its name of a member declared again", "{allOf: [{properties: {f: {type: object, " + f + "}}}, {properties: {f: {type: object}}}]}",
			"/allOf/0/properties/f/properties/f", "type/anonymous/components/schemas/M/allOf/0/properties/f", false},
		{"a type list lowering its own properties alone", "{type: [object, string], " + f + ", allOf: [{" + plain + "}]}", "/type/0/properties/f",
			"type/anonymous/components/schemas/M/type/0", false},
	} {
		model := tc.node
		if model == "" {
			model = componentID("M")
		}
		spec := "    M: " + tc.model + "\n    Plain: {type: object, properties: {z: {type: string}}}\n    Base: {type: object, " + plain + "}\n"
		doc, diags := parseFull(t, openapitest.ComponentSpec(spec))
		openapitest.RequireNoErrorDiags(t, diags)
		pointer := "/components/schemas/M" + tc.at
		if tc.onNode {
			node, ok := doc.Types[ir.TypeID("type/anonymous"+pointer)]
			require.True(t, ok, tc.name)
			assert.Equal(t, "DOC", node.Common().Docs.Description, "%s: the node holds the note", tc.name)
			assert.Empty(t, propertyOf(t, doc, "M", "f").Docs.Description, "%s: and the property none", tc.name)
			continue
		}
		assertNotesAt(t, doc, model, pointer, false, tc.name)
	}
}

// TestDeclaredAgain_ReadsEachSchemaComposingTheEntry pins the climb: each entry
// of a model that itself lists a name declared again keeps its notes on its
// node, whichever schema composing it lists the other declaration: the model
// itself, reached from its deepest entry its statement flattens, or an inner
// one where a reference makes that a model of its own past the depth the
// outermost flattens to. One no schema composing it declares again keeps them
// on its property, at any depth.
func TestDeclaredAgain_ReadsEachSchemaComposingTheEntry(t *testing.T) {
	t.Parallel()
	// nest wraps inner in depth allOf lists, and at is the pointer it stands at.
	nest := func(depth int, inner string) string {
		return strings.Repeat("{allOf: [", depth) + inner + strings.Repeat("]}", depth)
	}
	at := func(depth int) string { return "/components/schemas/M" + strings.Repeat("/allOf/0", depth) }
	f := "{properties: {f: " + noted + "}}"
	again := "{properties: {f: " + noted + "}, allOf: [" + f + "]}"
	const deepest = statement.MaxFlattenDepth + 1
	for _, tc := range []struct {
		name, model string
		named       int // how deep the entry a reference names is, or 0
		noted       []string
		onNode      bool
	}{
		{"the deepest entry the model lists, and its own", "{properties: {f: " + noted + "}, allOf: [" + nest(deepest-1, f) + "]}", 0,
			[]string{at(deepest) + "/properties/f", at(0) + "/properties/f"}, true},
		{"two of an inner model past that depth", nest(deepest+3, again), deepest - 1,
			[]string{at(deepest+3) + "/properties/f", at(deepest+4) + "/properties/f"}, true},
		{"one of an inner model past that depth", nest(deepest+3, f), deepest + 3, []string{at(deepest+3) + "/properties/f"}, false},
	} {
		spec, model := "    M: "+tc.model+"\n", componentID("M")
		if tc.named > 0 {
			spec += fmt.Sprintf("    User: {type: object, properties: {h: {$ref: '#%s'}}}\n", at(tc.named))
			model = ir.TypeID("type/anonymous" + at(tc.named))
		}
		doc, diags := parseFull(t, openapitest.ComponentSpec(spec))
		openapitest.RequireNoErrorDiags(t, diags)
		for _, pointer := range tc.noted {
			assertNotesAt(t, doc, model, pointer, tc.onNode, tc.name)
		}
	}
}

// TestMembersDeclaredAgain_AreTheMembersOneModelDeclaresMoreThanOnce pins what
// the finished run says of its members: those a model declares more than once
// itself, by the identity they settled with, and no other, a member one model
// declares and another brings among them.
func TestMembersDeclaredAgain_AreTheMembersOneModelDeclaresMoreThanOnce(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, spec string
		want       map[ir.PropertyID]bool
	}{
		{"declared again", "    M: {allOf: [{properties: {f: {type: string}, g: {type: string}}}, {properties: {f: {type: string}}}]}\n",
			map[ir.PropertyID]bool{"property/openapi/components/schemas/M/allOf/0/properties/f": true}},
		{"brought", "    M: {allOf: [{properties: {f: {type: string}}}, {$ref: '#/components/schemas/B'}]}\n    B: {properties: {f: {type: string}}}\n",
			map[ir.PropertyID]bool{}},
		{"declared once", "    M: {properties: {f: {type: string}}}\n", map[ir.PropertyID]bool{}},
	} {
		assert.Equal(t, tc.want, schema.MembersDeclaredAgainIn(t, openapitest.ComponentSpec(tc.spec)), tc.name)
	}
}
