// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema_test

import (
	"encoding/json/jsontext"
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/schema"
	"github.com/dexpace/morphic/compilers/openapi/internal/statement"
	"github.com/dexpace/morphic/ir"
)

// noted is a list, which builds a node of its own, described DOC, and notedF a
// schema declaring f as it.
const (
	noted  = "{type: array, items: {type: string}, description: DOC}"
	notedF = "properties: {f: " + noted + "}"
)

// notedOn says where the note DOC of the list declared at pointer, below the
// path of the document named doc ("" for the source), stands: on its node, on
// the property declared there, or on both.
func notedOn(t *testing.T, out *ir.Document, doc, pointer string) string {
	t.Helper()
	node, ok := out.Types[ir.TypeID("type/anonymous"+doc+pointer)]
	if !assert.True(t, ok, "%s builds a node", pointer) {
		return ""
	}
	var on []string
	if node.Common().Docs.Description == "DOC" {
		on = append(on, "node")
	}
	for _, td := range out.Types {
		model, isModel := td.(*ir.Model)
		if !isModel {
			continue
		}
		for _, property := range model.Properties {
			if property.ID == ir.PropertyID("property/openapi"+doc+pointer) && property.Docs.Description == "DOC" {
				on = append(on, "property")
			}
		}
	}
	slices.Sort(on)
	return strings.Join(slices.Compact(on), " and ")
}

// TestMayFold_ReadsEachDeclarationACompositionFolds pins which declarations of
// a member keep their notes on the nodes they build: each of several a model's
// composition decides it by, wherever the composition meets them, and no
// other, whose property holds them (docs/compilers/openapi.md#docs-and-deprecation).
// Every component reads alike, whichever is written first.
func TestMayFold_ReadsEachDeclarationACompositionFolds(t *testing.T) {
	t.Parallel()
	const model = "type: object, " + notedF
	// nest wraps inner in depth allOf lists.
	nest := func(depth int, inner string) string {
		return strings.Repeat("{allOf: [", depth) + inner + strings.Repeat("]}", depth)
	}
	for _, tc := range []struct {
		name       string
		components map[string]string
		folded     []string // below components/schemas, each declaration on its node
		carried    []string // each on its property
	}{
		{"declared once", map[string]string{"M": "{" + model + "}"}, nil, []string{"M/properties/f"}},
		{"declared once at the model and once at its entry's", map[string]string{
			"M": "{allOf: [" + refTo("A") + "], properties: {g: " + noted + "}}", "A": "{" + model + "}"},
			nil, []string{"M/properties/g", "A/properties/f"}},
		{"two inline entries", map[string]string{"M": "{allOf: [{" + notedF + "}, {" + notedF + "}]}"},
			[]string{"M/allOf/0/properties/f", "M/allOf/1/properties/f"}, nil},
		{"an inline entry and the model's own", map[string]string{"M": "{allOf: [{" + notedF + "}], " + notedF + "}"},
			[]string{"M/allOf/0/properties/f", "M/properties/f"}, nil},
		{"a nested entry and an entry", map[string]string{"M": "{allOf: [{allOf: [{" + notedF + "}]}, {" + notedF + "}]}"},
			[]string{"M/allOf/0/allOf/0/properties/f", "M/allOf/1/properties/f"}, nil},
		{"a wrapper's own and its entry", map[string]string{"M": "{allOf: [{allOf: [{" + notedF + "}], " + notedF + "}]}"},
			[]string{"M/allOf/0/allOf/0/properties/f", "M/allOf/0/properties/f"}, nil},
		{"the deepest entry a model lists and its own", map[string]string{
			"M": "{" + notedF + ", allOf: [" + nest(statement.MaxFlattenDepth, "{"+notedF+"}") + "]}"},
			[]string{"M/properties/f", "M" + strings.Repeat("/allOf/0", statement.MaxFlattenDepth+1) + "/properties/f"}, nil},
		{"the model's own and its entry's", map[string]string{
			"M": "{allOf: [" + refTo("A") + "], " + notedF + "}", "A": "{type: object, properties: {f: " + noted + ", g: " + noted + "}}"},
			[]string{"M/properties/f", "A/properties/f"}, []string{"A/properties/g"}},
		{"a nested entry and what another names", map[string]string{
			"M": "{allOf: [{allOf: [{" + notedF + "}]}, " + refTo("A") + "]}", "A": "{" + model + "}"},
			[]string{"M/allOf/0/allOf/0/properties/f", "A/properties/f"}, nil},
		{"the keywords beside an entry's $ref and what it names", map[string]string{
			"M": "{allOf: [{$ref: '#/components/schemas/A', " + notedF + "}]}", "A": "{" + model + "}"},
			[]string{"M/allOf/0/properties/f", "A/properties/f"}, nil},
		{"the model's own and what its $ref names", map[string]string{
			"M": "{$ref: '#/components/schemas/A', " + notedF + "}", "A": "{" + model + "}"},
			[]string{"M/properties/f", "A/properties/f"}, nil},
		{"two entries' targets", map[string]string{
			"M": "{allOf: [" + refTo("A") + ", " + refTo("B") + "]}", "A": "{" + model + "}", "B": "{" + model + "}"},
			[]string{"A/properties/f", "B/properties/f"}, nil},
		{"through a $ref naming a $ref", map[string]string{
			"M": "{allOf: [" + refTo("R") + "], " + notedF + "}", "R": refTo("S"), "S": refTo("A"), "A": "{" + model + "}"},
			[]string{"M/properties/f", "A/properties/f"}, nil},
		{"a chain of three models", map[string]string{
			"M": "{allOf: [" + refTo("B") + "], " + notedF + "}", "B": "{allOf: [" + refTo("A") + "], properties: {g: " + noted + "}}",
			"A": "{" + model + "}"},
			[]string{"M/properties/f", "A/properties/f"}, []string{"B/properties/g"}},
		{"a chain of four models", map[string]string{
			"M": "{allOf: [" + refTo("C") + ", {" + notedF + "}]}", "C": "{allOf: [" + refTo("B") + "]}",
			"B": "{allOf: [{allOf: [" + refTo("A") + "]}]}", "A": "{" + model + "}"},
			[]string{"M/allOf/1/properties/f", "A/properties/f"}, nil},
		{"one model by two paths", map[string]string{
			"M": "{allOf: [" + refTo("B") + ", " + refTo("C") + "]}", "B": "{allOf: [" + refTo("A") + "]}", "C": "{allOf: [" + refTo("A") + "]}",
			"A": "{" + model + "}"},
			nil, []string{"A/properties/f"}},
		{"one model by two paths, one declaring it", map[string]string{
			"M": "{allOf: [" + refTo("B") + ", " + refTo("C") + "]}", "B": "{allOf: [" + refTo("A") + "], " + notedF + "}",
			"C": "{allOf: [" + refTo("A") + "]}", "A": "{" + model + "}"},
			[]string{"A/properties/f", "B/properties/f"}, nil},
		{"a cycle of two models", map[string]string{
			"M": "{allOf: [" + refTo("A") + "], " + notedF + "}", "A": "{allOf: [" + refTo("M") + "], " + notedF + "}"},
			[]string{"M/properties/f", "A/properties/f"}, nil},
		{"a cycle declaring it once", map[string]string{
			"M": "{allOf: [" + refTo("A") + "], " + notedF + "}", "A": "{allOf: [" + refTo("M") + "], properties: {g: " + noted + "}}"},
			nil, []string{"M/properties/f", "A/properties/g"}},
		{"an entry a reference makes a model of its own", map[string]string{
			"M": "{allOf: [{" + notedF + "}, {$ref: '#/components/schemas/M/allOf/0'}]}"},
			[]string{"M/allOf/0/properties/f"}, nil},
		{"a union's branch and the model's own", map[string]string{
			"M": "{properties: {f: " + noted + ", g: " + noted + "}, oneOf: [{properties: {h: " + noted + "}}, {" + notedF + "}]}"},
			[]string{"M/properties/f", "M/oneOf/1/properties/f"}, []string{"M/properties/g", "M/oneOf/0/properties/h"}},
		{"an anyOf branch's target and an entry", map[string]string{
			"M": "{allOf: [{" + notedF + "}], anyOf: [" + refTo("A") + ", {type: object, properties: {h: {type: string}}}]}", "A": "{" + model + "}"},
			[]string{"M/allOf/0/properties/f", "A/properties/f"}, nil},
		{"a branch of an entry's union and the model's own", map[string]string{
			"M": "{allOf: [{oneOf: [" + refTo("A") + ", {type: object, properties: {h: {type: string}}}]}], " + notedF + "}", "A": "{" + model + "}"},
			[]string{"M/properties/f", "A/properties/f"}, nil},
		{"two branches of one union", map[string]string{"M": "{properties: {g: " + noted + "}, oneOf: [{" + notedF + "}, {" + notedF + "}]}"},
			nil, []string{"M/properties/g", "M/oneOf/0/properties/f", "M/oneOf/1/properties/f"}},
		{"the one branch of a nullable reference", map[string]string{
			"M": "{allOf: [" + refTo("N") + "], " + notedF + "}", "N": "{oneOf: [{type: 'null'}, " + refTo("A") + "]}", "A": "{" + model + "}"},
			[]string{"M/properties/f", "A/properties/f"}, nil},
		{"the one branch of an anyOf beside null", map[string]string{
			"M": "{allOf: [" + refTo("N") + "], " + notedF + "}", "N": "{anyOf: [" + refTo("A") + ", {type: 'null'}]}", "A": "{" + model + "}"},
			[]string{"M/properties/f", "A/properties/f"}, nil},
		{"a branch of a union an entry names", map[string]string{
			"M": "{allOf: [" + refTo("U") + "], " + notedF + "}", "U": "{anyOf: [" + refTo("A") + ", {type: object, properties: {h: {type: string}}}]}", "A": "{" + model + "}"},
			nil, []string{"M/properties/f", "A/properties/f"}},
		{"beside an entry whose reference names nothing", map[string]string{
			"M": "{allOf: [" + refTo("Missing") + "], " + notedF + "}"}, []string{"M/properties/f"}, nil},
		{"after an entry whose reference names nothing", map[string]string{
			"M": "{allOf: [" + refTo("Missing") + ", " + refTo("A") + "]}", "A": "{" + model + "}"}, []string{"A/properties/f"}, nil},
		{"before an entry whose reference names nothing", map[string]string{
			"M": "{allOf: [" + refTo("A") + ", " + refTo("Missing") + "]}", "A": "{" + model + "}"}, []string{"A/properties/f"}, nil},
	} {
		names := slices.Sorted(maps.Keys(tc.components))
		for order := range 2 {
			if order == 1 {
				slices.Reverse(names)
			}
			var spec strings.Builder
			for _, name := range names {
				spec.WriteString("    " + name + ": " + tc.components[name] + "\n")
			}
			doc, _ := parseFull(t, openapitest.ComponentSpec(spec.String()))
			for _, at := range tc.folded {
				assert.Equal(t, "node", notedOn(t, doc, "", "/components/schemas/"+at), "%s (order %d): %s", tc.name, order, at)
			}
			for _, at := range tc.carried {
				assert.Equal(t, "property", notedOn(t, doc, "", "/components/schemas/"+at), "%s (order %d): %s", tc.name, order, at)
			}
		}
	}
}

// TestMayFold_ReadsTheDeclarationsOfWhatAFoldComposes pins that the members of
// the types a fold judges one against another are read as a composition's are:
// two objects' own, at every position the types hold values, at each depth,
// and through a union's variant, a reference and a dynamic one. A member one of
// the types alone declares keeps its notes on its property.
func TestMayFold_ReadsTheDeclarationsOfWhatAFoldComposes(t *testing.T) {
	t.Parallel()
	const object = "{type: object, properties: {p: " + noted + "}}"
	const other = "{type: object, properties: {p: " + noted + ", q: " + noted + "}}"
	const draft = "jsonSchemaDialect: 'http://json-schema.org/draft-07/schema#'\n"
	for _, tc := range []struct {
		name, f, g string // f as M's entry declares it, and as A does
		at, atG    string // where the two declare p, below those
		dialect    string // what the root writes of a dialect
	}{
		{"objects", object, other, "", "", ""},
		{"objects of an object", "{type: object, properties: {o: " + object + "}}", "{type: object, properties: {o: " + other + "}}",
			"/properties/o", "/properties/o", ""},
		{"a list's elements", "{type: array, items: " + object + "}", "{type: array, items: " + other + "}", "/items", "/items", ""},
		{"a tuple's positions", "{type: array, prefixItems: [{type: string}, " + object + "]}", "{type: array, prefixItems: [{type: string}, " + other + "]}",
			"/prefixItems/1", "/prefixItems/1", ""},
		{"what follows a tuple's", "{type: array, prefixItems: [{type: string}], items: " + object + "}",
			"{type: array, prefixItems: [{type: string}], items: " + other + "}", "/items", "/items", ""},
		{"a map's values", "{type: object, additionalProperties: " + object + "}", "{type: object, additionalProperties: " + other + "}",
			"/additionalProperties", "/additionalProperties", ""},
		{"a pattern's values", "{type: object, patternProperties: {'^a': " + object + "}}", "{type: object, patternProperties: {'^a': " + other + "}}",
			"/patternProperties/^a", "/patternProperties/^a", ""},
		{"a tuple a draft lists", "{type: array, items: [" + object + "]}",
			"{type: array, items: [" + other + "]}", "/items/0", "/items/0", draft},
		{"what follows a tuple a draft lists", "{type: array, items: [{type: string}], additionalItems: " + object + "}",
			"{type: array, items: [{type: string}], additionalItems: " + other + "}",
			"/additionalItems", "/additionalItems", draft},
		{"a oneOf's variant", "{oneOf: [{type: string}, " + object + "]}", other, "/oneOf/1", "", ""},
		{"an anyOf's variant", "{anyOf: [{type: string}, " + object + "]}", other, "/anyOf/1", "", ""},
		{"an entry's object", "{allOf: [" + refTo("Plain") + ", " + object + "]}", other, "/allOf/1", "", ""},
	} {
		spec := "    M: {allOf: [{properties: {f: " + tc.f + "}}, " + refTo("A") + "]}\n" +
			"    A: {type: object, properties: {f: " + tc.g + "}}\n    Plain: {type: object, properties: {z: {type: string}}}\n"
		doc, diags := parseFull(t, tc.dialect+openapitest.ComponentSpec(spec))
		openapitest.RequireNoErrorDiags(t, diags)
		assert.Equal(t, "node", notedOn(t, doc, "", "/components/schemas/M/allOf/0/properties/f"+tc.at+"/properties/p"), tc.name)
		assert.Equal(t, "node", notedOn(t, doc, "", "/components/schemas/A/properties/f"+tc.atG+"/properties/p"), tc.name)
		assert.Equal(t, "property", notedOn(t, doc, "", "/components/schemas/A/properties/f"+tc.atG+"/properties/q"), tc.name)
	}
	// A type no other meets is judged against none: its variants' members are
	// each declared once.
	alone, _ := parseFull(t, openapitest.ComponentSpec("    M: {allOf: [{properties: {f: {type: array, items: {oneOf: ["+object+", "+other+"]}}}}, "+
		refTo("A")+"]}\n    A: {type: object, properties: {f: {type: array}}}\n"))
	for _, variant := range []string{"/oneOf/0", "/oneOf/1"} {
		assert.Equal(t, "property", notedOn(t, alone, "", "/components/schemas/M/allOf/0/properties/f/items"+variant+"/properties/p"), variant)
	}

	// Two tuples' values are judged position by position.
	apart, _ := parseFull(t, openapitest.ComponentSpec("    M: {allOf: [{properties: {f: {type: array, prefixItems: ["+object+", {type: string}]}}}, "+
		refTo("A")+"]}\n    A: {type: object, properties: {f: {type: array, prefixItems: [{type: string}, "+object+"]}}}\n"))
	assert.Equal(t, "property", notedOn(t, apart, "", "/components/schemas/M/allOf/0/properties/f/prefixItems/0/properties/p"))
	assert.Equal(t, "property", notedOn(t, apart, "", "/components/schemas/A/properties/f/prefixItems/1/properties/p"))

	named := "    M: {allOf: [{properties: {f: %s}}, " + refTo("A") + "]}\n" +
		"    A: {type: object, properties: {f: " + object + "}}\n    N: {$dynamicAnchor: n, type: object, properties: {p: " + noted + "}}\n"
	for _, ref := range []string{refTo("N"), "{$dynamicRef: '#n'}", "{allOf: [" + refTo("N") + "]}"} {
		doc, diags := parseFull(t, openapitest.ComponentSpecVer("3.1.0", fmt.Sprintf(named, ref)))
		openapitest.RequireNoErrorDiags(t, diags)
		assert.Equal(t, "node", notedOn(t, doc, "", "/components/schemas/N/properties/p"), ref)
		assert.Equal(t, "node", notedOn(t, doc, "", "/components/schemas/A/properties/f/properties/p"), ref)
	}
}

// TestMayFold_ReadsAModelOfAnotherDocument pins that a declaration in another
// document a composition reaches keeps its notes on its node, as the source's
// own beside it does, and one no composition meets there keeps them on its
// property.
func TestMayFold_ReadsAModelOfAnotherDocument(t *testing.T) {
	t.Parallel()
	other := "A: {type: object, properties: {f: " + noted + ", g: " + noted + "}}\n"
	root := openapitest.ComponentSpec("    M: {allOf: [{$ref: 'other.yaml#/A'}], " + notedF + "}\n")
	doc, diags, _ := compileBeside(t, map[string]string{"other.yaml": other}, root)
	openapitest.RequireNoErrorDiags(t, diags)
	assert.Equal(t, "node", notedOn(t, doc, "", "/components/schemas/M/properties/f"))
	assert.Equal(t, "node", notedOn(t, doc, "@other.yaml", "/A/properties/f"))
	assert.Equal(t, "property", notedOn(t, doc, "@other.yaml", "/A/properties/g"))
}

// TestMayFold_PastItsReadsEveryDeclarationMayFold pins the bound: a reading
// within its limit says what each declaration is, and one cut short says that
// any may fold, a chain of models being read whole at each of them.
func TestMayFold_PastItsReadsEveryDeclarationMayFold(t *testing.T) {
	t.Parallel()
	const folded, alone = jsontext.Pointer("/components/schemas/M/properties/f"), jsontext.Pointer("/components/schemas/Z/properties/f")
	// Two schemas declaring one property each and the reference between them,
	// then the types of the two declarations and those of their elements, each
	// queued and read: one read a schema, a property and a type queued.
	const reads = 2*2 + 1 + 2*2 + 2*2
	src := openapitest.ComponentSpec("    M: {allOf: [" + refTo("A") + "], " + notedF + "}\n    A: {type: object, " + notedF + "}\n" +
		"    Z: {type: object, " + notedF + "}\n")
	within := schema.MayFoldWithin(t, src, budget.AtMost(reads))
	assert.True(t, within(folded))
	assert.False(t, within(alone), "declared once")
	past := schema.MayFoldWithin(t, src, budget.AtMost(reads-1))
	assert.True(t, past(folded))
	assert.True(t, past(alone), "cut short, every declaration may fold")
	assert.False(t, past("/components/parameters/P/schema"), "a parameter folds with none")

	var chain strings.Builder
	chain.WriteString("    Z: {type: object, " + notedF + "}\n    C0: {type: object}\n")
	const models = 50
	for i := range models {
		fmt.Fprintf(&chain, "    C%d: {allOf: [%s]}\n", i+1, refTo(fmt.Sprintf("C%d", i)))
	}
	ofChain := openapitest.ComponentSpec(chain.String())
	// Each model of the chain reads itself, all beneath it and the references to them.
	const chainReads = models*(models+1) + models
	assert.False(t, schema.MayFoldWithin(t, ofChain, budget.AtMost(chainReads))(alone))
	assert.True(t, schema.MayFoldWithin(t, ofChain, budget.AtMost(chainReads-1))(alone))
	assert.Equal(t, 1<<22, schema.MaxConjunctReads, "what a run reads within")
}

// TestMayFold_ReadsATypeGroupOnce pins that the types of one member are read
// against each other once, however many compositions meet them and in whatever
// order: one holding its own member of one name would otherwise be read without
// end.
func TestMayFold_ReadsATypeGroupOnce(t *testing.T) {
	t.Parallel()
	src := openapitest.ComponentSpec("    A: {type: object, properties: {child: " + refTo("A") + "}}\n" +
		"    B: {allOf: [" + refTo("A") + "], properties: {child: " + refTo("B") + ", x: " + noted + "}}\n" +
		"    C: {allOf: [" + refTo("B") + "], properties: {child: " + refTo("C") + ", x: " + noted + "}}\n" +
		"    Z: {type: object, " + notedF + "}\n")
	within := schema.MayFoldWithin(t, src, budget.AtMost(200))
	assert.False(t, within("/components/schemas/Z/properties/f"), "the reading ends")
	assert.True(t, within("/components/schemas/B/properties/x"))
	assert.True(t, within("/components/schemas/C/properties/x"))

	// Each of two models reads itself, two references and the two models they
	// name, one property each, and queues the two types, which are read once and
	// then their two elements', queued and read once.
	const reads = 2*(1+2+2+2+2) + 2 + 2 + 2
	both := openapitest.ComponentSpec("    M: {allOf: [" + refTo("A") + ", " + refTo("B") + "]}\n    N: {allOf: [" + refTo("B") + ", " + refTo("A") + "]}\n" +
		"    A: {type: object, " + notedF + "}\n    B: {type: object, " + notedF + "}\n    Z: {type: object, " + notedF + "}\n")
	assert.False(t, schema.MayFoldWithin(t, both, budget.AtMost(reads))("/components/schemas/Z/properties/f"))
	assert.True(t, schema.MayFoldWithin(t, both, budget.AtMost(reads-1))("/components/schemas/Z/properties/f"))
}

// TestCarryingMembers_AreThePropertiesHoldingTheirSchemasNotes pins what the
// run says of its properties: those holding notes of schemas that built nodes,
// and no other, one writing none, one building no node, one whose declaration a
// composition folds and one a reference names among them.
func TestCarryingMembers_AreThePropertiesHoldingTheirSchemasNotes(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, property string }{
		{"described", "{type: array, items: {}, description: d}"},
		{"deprecated", "{type: array, items: {}, deprecated: true}"},
		{"exemplified", "{type: array, items: {}, example: []}"},
		{"deprecated by a key", "{type: array, items: {}, x-fern-availability: deprecated}"},
	} {
		spec := "    M: {properties: {f: " + tc.property + ", silent: {type: array, items: {}}, plain: {type: string, description: d}}}\n" +
			"    N: {allOf: [{properties: {f: " + tc.property + "}}, {properties: {f: " + tc.property + "}}]}\n" +
			"    R: {properties: {f: " + tc.property + "}}\n    U: {properties: {f: {$ref: '#/components/schemas/R/properties/f'}}}\n"
		want := map[ir.PropertyID]bool{"property/openapi/components/schemas/M/properties/f": true}
		assert.Equal(t, want, schema.CarryingMembersIn(t, openapitest.ComponentSpec(spec)), tc.name)
	}
}

// TestSettleMembers_ReportsAFoldedDeclarationHoldingNotes pins that the fold of
// a member is told which properties hold notes off their nodes: one of its
// declarations recorded so is reported as a compiler bug, and no declaration
// the lowering itself leaves so is.
func TestSettleMembers_ReportsAFoldedDeclarationHoldingNotes(t *testing.T) {
	t.Parallel()
	src := openapitest.ComponentSpec("    M: {allOf: [" + refTo("A") + "], " + notedF + "}\n    A: {type: object, " + notedF + "}\n")
	for _, tc := range []struct {
		property ir.PropertyID
		at       []jsontext.Pointer
	}{
		{"property/none", nil},
		{"property/openapi/components/schemas/M/properties/f", []jsontext.Pointer{"/components/schemas/M/properties/f"}},
		{"property/openapi/components/schemas/A/properties/f", []jsontext.Pointer{"/components/schemas/M/allOf/0"}},
	} {
		diags := schema.SettledHoldingNotes(t, src, tc.property)
		assert.ElementsMatch(t, tc.at, openapitest.PointersOf(diags, diag.InternalInvariant), tc.property)
	}
}
