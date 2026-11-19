// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/ir"
)

// conjunctionPositions are where conjunctionSpec writes the schema under
// test: a component, a property, a list's element and a query parameter.
var conjunctionPositions = []jsontext.Pointer{
	"/components/schemas/Pos",
	"/components/schemas/Holder/properties/p",
	"/components/schemas/Els/items",
	"/paths/~1x/get/parameters/0/schema",
}

// conjunctionSpec is a document of the version given writing schema, one line
// of flow YAML, at each of conjunctionPositions, beside the components named.
func conjunctionSpec(version, schema string, components ...string) string {
	paths := "paths:\n  /x:\n    get:\n      operationId: x\n      parameters:\n" +
		"        - {name: q, in: query, schema: " + schema + "}\n" +
		"      responses: {\"204\": {description: ok}}\n"
	return admissionSpec(version, paths, append([]string{
		"Pos: " + schema,
		"Holder: {type: object, properties: {p: " + schema + "}}",
		"Els: {type: array, items: " + schema + "}",
	}, components...)...)
}

// usedAt returns what the position written at at lowers to, past any alias the
// position owns: the node or primitive a reader of the position renders.
func usedAt(t *testing.T, doc *ir.Document, at jsontext.Pointer) ir.TypeID {
	t.Helper()
	var id ir.TypeID
	switch at {
	case conjunctionPositions[0]:
		id = componentID("Pos")
	case conjunctionPositions[1]:
		holder, ok := kindNode(t, doc, componentID("Holder")).(*ir.Model)
		require.True(t, ok)
		property, ok := propertyByWire(holder, "p")
		require.True(t, ok)
		id = property.Type.Target
	case conjunctionPositions[2]:
		list, ok := kindNode(t, doc, componentID("Els")).(*ir.List)
		require.True(t, ok)
		id = list.ElementType.Target
	default:
		operation := operationAt(t, doc, "GET", "/x")
		require.Len(t, operation.Params, 1)
		id = operation.Params[0].Type.Target
	}
	for range 4 {
		alias, ok := doc.Types[id].(*ir.Scalar)
		if !ok || alias.Base == nil || alias.Provenance.Pointer != at {
			return id
		}
		id = alias.Base.Target
	}
	return id
}

// keptUnder returns the reason of each Unmodeled entry of doc kept from a
// keyword written at or under at, by the pointer of what it keeps.
func keptUnder(doc *ir.Document, at jsontext.Pointer) map[jsontext.Pointer]ir.UnmodeledReason {
	out := map[jsontext.Pointer]ir.UnmodeledReason{}
	for _, site := range unmodeledSites(doc) {
		pointer := site.entry.Provenance.Pointer
		if pointer == at || strings.HasPrefix(string(pointer), string(at)+"/") {
			out[pointer] = site.entry.Reason
		}
	}
	return out
}

// degradedUnder lists, sorted, the pointers at or under at that an info
// openapi/degraded-construct is drawn at, once per info.
func degradedUnder(diags []ir.Diagnostic, at jsontext.Pointer) []jsontext.Pointer {
	return slices.DeleteFunc(degradedInfoPointers(diags), func(pointer jsontext.Pointer) bool {
		return pointer != at && !strings.HasPrefix(string(pointer), string(at)+"/")
	})
}

// markedNodes lists, sorted, the IDs of the nodes of doc naming the
// kind-inference policy.
func markedNodes(doc *ir.Document) []ir.TypeID {
	var out []ir.TypeID
	for id, td := range doc.Types {
		if markedKindInference(td) {
			out = append(out, id)
		}
	}
	slices.Sort(out)
	return out
}

// entriesSwapped is comparableDocument of doc with the pointers and keys of an
// allOf's two entries swapped, so a document compiled from the entries in one
// order compares equal to the one compiled from them in the other.
func entriesSwapped(t *testing.T, doc *ir.Document) string {
	t.Helper()
	raw, err := json.Marshal(doc)
	require.NoError(t, err)
	swapped := strings.NewReplacer("allOf/0", "allOf/1", "allOf/1", "allOf/0").Replace(string(raw))
	var back ir.Document
	require.NoError(t, json.Unmarshal([]byte(swapped), &back))
	return comparableDocument(t, &back)
}

// inBothMappingOrders runs check over spec as written and over spec with every
// mapping's entries reversed: the components, the keywords of each schema and
// the members of each object, so neither order decides the document.
func inBothMappingOrders(t *testing.T, spec string, check func(t *testing.T, spec string)) {
	t.Helper()
	t.Run("declared", func(t *testing.T) { check(t, spec) })
	t.Run("reversed", func(t *testing.T) { check(t, string(reversed(t, spec))) })
}

// declaredBesideClaim are schemas declaring a string beside an allOf entry
// whose keywords claim an array, the entry at the index given.
var declaredBesideClaim = []struct {
	name, schema string
	entry        int
}{
	{"entry last", "{allOf: [{type: string}, {items: {type: string}}]}", 1},
	{"entry first", "{allOf: [{items: {type: string}}, {type: string}]}", 0},
	{"type beside", "{type: string, allOf: [{items: {type: string}}]}", 0},
}

// TestKindInference_ADeclaredKindDecidesBesideAnEntryTheKeywordsClaim holds
// that an allOf entry writing `items` and no type claims no kind against a
// conjunct declaring one: JSON Schema applies `items` to arrays alone, so the
// conjunction admits every string. At every position, in both entry orders and
// under either policy, the position is a use of the string, the entry's items
// is kept under its own key with one info at the entry, nothing is marked and
// nothing warns.
func TestKindInference_ADeclaredKindDecidesBesideAnEntryTheKeywordsClaim(t *testing.T) {
	t.Parallel()
	for _, version := range []string{"3.0.3", "3.1.0"} {
		for _, off := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/off=%t", version, off), func(t *testing.T) {
				t.Parallel()
				for _, tc := range declaredBesideClaim {
					inBothMappingOrders(t, conjunctionSpec(version, tc.schema), func(t *testing.T, spec string) {
						doc, diags := compileKindInference(t, "declared", spec, off)
						requireErrorsAndWarnings(t, diags, nil)
						assert.Empty(t, markedNodes(doc), tc.name)
						for _, at := range conjunctionPositions {
							entry := at + jsontext.Pointer(fmt.Sprintf("/allOf/%d", tc.entry))
							assert.Equal(t, primitive(ir.PrimitiveString), usedAt(t, doc, at), "%s at %s", tc.name, at)
							assert.Equal(t, map[jsontext.Pointer]ir.UnmodeledReason{entry + "/items": ir.ReasonDegradedLowering},
								keptUnder(doc, at), "%s at %s", tc.name, at)
							assert.Equal(t, []jsontext.Pointer{entry}, degradedUnder(diags, at), "%s at %s", tc.name, at)
						}
					})
				}
			})
		}
	}
}

// TestKindInference_TheEntryOrderAndThePolicyLeaveTheDeclaredKindAlone holds
// the documents of a string declared beside an entry claiming an array to one
// another: the entries in either order compile to one document but for their
// indices, and the policy on compiles to what it compiles to off, since the
// document declares the kind.
func TestKindInference_TheEntryOrderAndThePolicyLeaveTheDeclaredKindAlone(t *testing.T) {
	t.Parallel()
	for _, version := range []string{"3.0.3", "3.1.0"} {
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			last, _ := compileKindInference(t, "order", conjunctionSpec(version, declaredBesideClaim[0].schema), false)
			first, _ := compileKindInference(t, "order", conjunctionSpec(version, declaredBesideClaim[1].schema), false)
			off, _ := compileKindInference(t, "order", conjunctionSpec(version, declaredBesideClaim[0].schema), true)
			assert.Equal(t, comparableDocument(t, last), entriesSwapped(t, first), "the entry order decided the document")
			assert.Equal(t, comparableDocument(t, off), comparableDocument(t, last), "the policy decided a declared kind")
		})
	}
}

// claimedTargets are components writing shape keywords and no type: E and P,
// a list and a model by the policy's reading, and R, whose `required` speaks of
// an object and infers no kind, so the policy reads none.
var claimedTargets = []string{
	"E: {items: {type: string}}",
	"R: {required: [a]}",
	"P: {properties: {a: {type: string}}}",
}

// inferredTargets are the claimedTargets whose kind the policy reads.
var inferredTargets = map[string]bool{"E": true, "P": true}

// TestKindInference_ADeclaredKindOutvotesARefTheKeywordsClaim holds an allOf
// conjoining a string with a $ref to a schema the policy reads as another
// kind: the document declares the string, so the position is a use of it, the
// $ref entry is kept with one info naming the policy, nothing warns and the
// target is still the node the policy reads it as. With the policy off, or a
// target whose keywords infer no kind, the target is the top type, which says
// nothing of the kind: the string decides as well, the info saying the target
// admits every kind.
func TestKindInference_ADeclaredKindOutvotesARefTheKeywordsClaim(t *testing.T) {
	t.Parallel()
	for _, version := range []string{"3.0.3", "3.1.0"} {
		for _, target := range []string{"E", "R", "P"} {
			ref := "{$ref: '#/components/schemas/" + target + "'}"
			for _, refAt := range []int{0, 1} {
				entries := []string{ref, "{type: string}"}
				if refAt == 1 {
					slices.Reverse(entries)
				}
				schema := "{allOf: [" + strings.Join(entries, ", ") + "]}"
				t.Run(fmt.Sprintf("%s/%s/ref at %d", version, target, refAt), func(t *testing.T) {
					t.Parallel()
					inBothMappingOrders(t, conjunctionSpec(version, schema, claimedTargets...), func(t *testing.T, spec string) {
						assertOutvotedRef(t, spec, target, refAt)
					})
				})
			}
		}
	}
}

// assertOutvotedRef holds spec, whose positions conjoin a string with a $ref to
// target at the entry refAt, to what each policy lowers it to.
func assertOutvotedRef(t *testing.T, spec, target string, refAt int) {
	t.Helper()
	for _, off := range []bool{false, true} {
		inferred := !off && inferredTargets[target]
		doc, diags := compileKindInference(t, "outvoted", spec, off)
		requireErrorsAndWarnings(t, diags, nil)
		assert.Equal(t, inferred, markedKindInference(kindNode(t, doc, componentID(target))), "off=%t", off)
		why := "admits every kind"
		if inferred {
			why = "kind-inference"
		}
		for _, at := range conjunctionPositions {
			entry := at + jsontext.Pointer(fmt.Sprintf("/allOf/%d", refAt))
			assert.Equal(t, primitive(ir.PrimitiveString), usedAt(t, doc, at), "off=%t at %s", off, at)
			assert.Equal(t, map[jsontext.Pointer]ir.UnmodeledReason{entry: ir.ReasonDegradedLowering},
				keptUnder(doc, at), "off=%t at %s", off, at)
			assert.Equal(t, []jsontext.Pointer{entry}, degradedUnder(diags, at), "off=%t at %s", off, at)
			assert.Contains(t, diagMessageOf(t, diags, string(entry)), why, "the info says why at %s", at)
		}
	}
}

// TestKindInference_AUseOfAnInferredKindIsAnAliasOfIt holds an allOf whose one
// $ref names a schema the policy reads as a list: nothing beside it declares
// another kind, so the position is a use of the list, holding the bound
// written beside it and keeping nothing, a `type: array` beside it among what
// the list already says. With the policy off the target is the top type: the
// position is a use of it holding the bound, and beside a declared array the
// list the entry declaring it builds, keeping the $ref, which says nothing of
// the kind.
func TestKindInference_AUseOfAnInferredKindIsAnAliasOfIt(t *testing.T) {
	t.Parallel()
	ref := "{$ref: '#/components/schemas/E'}"
	uses := []struct {
		name, a, b, beside string
		minItems           int
		off                ir.TypeKind
		offKept            bool
	}{
		{"alone", ref, "", ", description: d", 0, ir.KindScalar, false},
		{"bound", ref, "{minItems: 1}", "", 1, ir.KindScalar, false},
		{"typed bound", ref, "{type: array, minItems: 1}", "", 1, ir.KindList, true},
	}
	for _, version := range []string{"3.0.3", "3.1.0"} {
		for _, tc := range uses {
			orders := [][]string{{tc.a}}
			if tc.b != "" {
				orders = [][]string{{tc.a, tc.b}, {tc.b, tc.a}}
			}
			for _, entries := range orders {
				schema := "{allOf: [" + strings.Join(entries, ", ") + "]" + tc.beside + "}"
				t.Run(version+"/"+schema, func(t *testing.T) {
					t.Parallel()
					inBothOrders(t, []string{"Pos: " + schema, claimedTargets[0]}, func(t *testing.T, schemas []string) {
						spec := admissionSpec(version, "", schemas...)
						on, onDiags := compileKindInference(t, "use", spec, false)
						requireErrorsAndWarnings(t, onDiags, nil)
						alias, ok := kindNode(t, on, componentID("Pos")).(*ir.Scalar)
						require.True(t, ok, "a use of E")
						require.NotNil(t, alias.Base)
						assert.Equal(t, componentID("E"), alias.Base.Target)
						assert.Equal(t, ir.KindList, kindNode(t, on, componentID("E")).Kind())
						assert.Empty(t, keptUnder(on, conjunctionPositions[0]), "the list says all its conjuncts do")
						assert.Empty(t, degradedUnder(onDiags, conjunctionPositions[0]))
						assertMinItems(t, alias.Constraints, tc.minItems)
						off, offDiags := compileKindInference(t, "use", spec, true)
						requireErrorsAndWarnings(t, offDiags, missingItemsOf(version, tc.offKept, entries, tc.b))
						assert.Equal(t, tc.off, kindNode(t, off, componentID("Pos")).Kind())
						assert.Equal(t, tc.offKept, len(keptUnder(off, conjunctionPositions[0])) == 1)
					})
				})
			}
		}
	}
}

// missingItemsOf is the warning a 3.0 array writing no items draws where the
// entry typed, written among entries, is what the position is a use of, the
// $ref beside it kept.
func missingItemsOf(version string, kept bool, entries []string, typed string) []wantFinding {
	if version != "3.0.3" || !kept {
		return nil
	}
	return []wantFinding{{code: diag.MissingArrayItems, pointer: fmt.Sprintf("/components/schemas/Pos/allOf/%d",
		slices.Index(entries, typed))}}
}

// assertMinItems holds constraints to a minItems of want, and to none where
// want is 0.
func assertMinItems(t *testing.T, constraints *ir.Constraints, want int) {
	t.Helper()
	if want == 0 {
		assert.True(t, constraints == nil || constraints.MinItems == nil, "no minItems")
		return
	}
	require.NotNil(t, constraints)
	require.NotNil(t, constraints.MinItems)
	assert.Equal(t, int64(want), *constraints.MinItems)
}

// TestKindInference_ACompositionIsMarkedWhereNoConjunctDeclaresItsKind holds
// the marker on an allOf composed as a model: one whose conjuncts name the
// object only by the policy's reading of their keywords, two untyped $ref
// targets or untyped entries, names the policy, and one a conjunct declares an
// object beside does not. With the policy off no node is marked. Entries
// speaking of an object and an array compose no model at all
// (TestAllOfCompose_EntriesClaimingTwoKindsAreNotAModel).
func TestKindInference_ACompositionIsMarkedWhereNoConjunctDeclaresItsKind(t *testing.T) {
	t.Parallel()
	targets := []string{"P: {properties: {a: {type: string}}}", "Q: {properties: {b: {type: string}}}"}
	compositions := []struct {
		name, a, b string
		marked     bool
	}{
		{"Declared", "{$ref: '#/components/schemas/P'}", "{type: object}", false},
		{"TwoRefs", "{$ref: '#/components/schemas/P'}", "{$ref: '#/components/schemas/Q'}", true},
		{"Required", "{properties: {a: {type: string}}}", "{required: [a]}", true},
	}
	for _, version := range []string{"3.0.3", "3.1.0"} {
		for _, swap := range []bool{false, true} {
			schemas := slices.Clone(targets)
			for _, tc := range compositions {
				a, b := tc.a, tc.b
				if swap {
					a, b = b, a
				}
				schemas = append(schemas, tc.name+": {allOf: ["+a+", "+b+"]}")
			}
			t.Run(fmt.Sprintf("%s/swapped=%t", version, swap), func(t *testing.T) {
				t.Parallel()
				inBothOrders(t, schemas, func(t *testing.T, schemas []string) {
					on, _ := compileKindInference(t, "marker", admissionSpec(version, "", schemas...), false)
					for _, tc := range compositions {
						td := kindNode(t, on, componentID(tc.name))
						assert.Equal(t, ir.KindModel, td.Kind(), tc.name)
						assert.Equal(t, tc.marked, markedKindInference(td), tc.name)
					}
					off, _ := compileKindInference(t, "marker", admissionSpec(version, "", schemas...), true)
					assert.Empty(t, markedNodes(off), "the policy off reads no kind")
				})
			})
		}
	}
}

// TestKindInference_AnEntryReadAloneIsItsOwnPosition holds an allOf entry
// writing `items` beside a declared string, named by a $ref from elsewhere:
// read alone it is a position of its own, a marked list, while the allOf
// writing it stays the string, two nodes whichever is declared first.
func TestKindInference_AnEntryReadAloneIsItsOwnPosition(t *testing.T) {
	t.Parallel()
	schemas := []string{
		"Out: {$ref: '#/components/schemas/Pos/allOf/1'}",
		"Pos: " + declaredBesideClaim[0].schema,
	}
	entryID := ir.TypeID("type/anonymous/components/schemas/Pos/allOf/1")
	for _, version := range []string{"3.0.3", "3.1.0"} {
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			inBothOrders(t, schemas, func(t *testing.T, schemas []string) {
				for _, off := range []bool{false, true} {
					doc, diags := compileKindInference(t, "entry", admissionSpec(version, "", schemas...), off)
					requireErrorsAndWarnings(t, diags, nil)
					entry := kindNode(t, doc, entryID)
					out, ok := kindNode(t, doc, componentID("Out")).(*ir.Scalar)
					require.True(t, ok)
					require.NotNil(t, out.Base)
					assert.Equal(t, entryID, out.Base.Target, "off=%t", off)
					assert.Equal(t, primitive(ir.PrimitiveString), usedAt(t, doc, conjunctionPositions[0]), "off=%t", off)
					assert.Equal(t, !off, entry.Kind() == ir.KindList, "off=%t: %s", off, entry.Kind())
					assert.Equal(t, !off, markedKindInference(entry), "off=%t", off)
				}
			})
		})
	}
}

// TestKindInference_AFormBodyKeysNoPartItsModelLacks holds a multipart body
// conjoining a string with an entry declaring a property: every part the
// request content keys an encoding by is a property a model of the document
// holds, under either policy.
func TestKindInference_AFormBodyKeysNoPartItsModelLacks(t *testing.T) {
	t.Parallel()
	const body = `paths:
  /b:
    post:
      operationId: b
      requestBody:
        content:
          multipart/form-data:
            schema: {allOf: [{type: string}, {properties: {f: {type: string, format: binary}}}]}
      responses: {"204": {description: d}}
`
	for _, version := range []string{"3.0.3", "3.1.0"} {
		for _, off := range []bool{false, true} {
			doc, _ := compileKindInference(t, "form", admissionSpec(version, body), off)
			held := map[ir.PropertyID]bool{}
			for _, td := range doc.Types {
				if model, ok := td.(*ir.Model); ok {
					for _, property := range model.Properties {
						held[property.ID] = true
					}
				}
			}
			content := requestContent(t, operationAt(t, doc, "POST", "/b"))
			for _, part := range slices.Sorted(maps.Keys(content.Encoding)) {
				assert.True(t, held[part], "%s off=%t: the part %s is no property of the document", version, off, part)
			}
		}
	}
}

// restatingObjects is a union whose branches each declare an object and only
// narrow it, closing a schema written beside it.
const restatingObjects = "oneOf: [{type: object, required: [a]}, {type: object, required: [b]}]}"

// TestKindInference_WhatAPositionSaysIsReadOffWhatItLowersTo holds the readers
// asking whether a position already says what a keyword does to the node what
// it compares to lowers to: a property set the policy reads as an object, as a
// property or an allOf entry of a body, restates `type: object`, a branch
// writing `items` admits no string beside a string the allOf lowers to, and
// branches the policy reads as objects take a bare `required`; with the policy
// off, each is the top type. Keywords beside branches each declaring an object
// are that object under either policy, as beside `type: object`.
func TestKindInference_WhatAPositionSaysIsReadOffWhatItLowersTo(t *testing.T) {
	t.Parallel()
	schemas := []string{
		"Nested: {type: object, properties: {a: {properties: {x: {type: string}}}}, " +
			"oneOf: [{properties: {a: {type: object}}, required: [a]}]}",
		"HeldUnion: {allOf: [{type: string}], oneOf: [{type: string}, {items: {type: string}}]}",
		"BareRequired: {oneOf: [{properties: {a: {type: string}}}, {properties: {b: {type: string}}}], required: [a]}",
		"P: {properties: {a: {type: string}}}",
		"Entry: {allOf: [{properties: {a: {type: string}}}], oneOf: [{type: object, required: [a]}]}",
		"RefEntry: {allOf: [{$ref: '#/components/schemas/P'}], oneOf: [{type: object, required: [a]}]}",
		"Closed: {additionalProperties: false, " + restatingObjects,
		"Required: {required: [c], " + restatingObjects,
		"Typed: {properties: {a: {type: string}}, " + restatingObjects,
	}
	type outcome struct {
		kind ir.TypeKind
		kept map[string]ir.UnmodeledReason
	}
	want := map[bool]map[string]outcome{
		false: {
			"Nested":       {ir.KindModel, map[string]ir.UnmodeledReason{"openapi:oneOf": ir.ReasonValidationOnly}},
			"HeldUnion":    {ir.KindScalar, map[string]ir.UnmodeledReason{}},
			"BareRequired": {ir.KindUnion, map[string]ir.UnmodeledReason{}},
			"Entry":        {ir.KindModel, map[string]ir.UnmodeledReason{"openapi:oneOf": ir.ReasonValidationOnly}},
			"RefEntry":     {ir.KindModel, map[string]ir.UnmodeledReason{"openapi:oneOf": ir.ReasonValidationOnly}},
		},
		true: {
			"Nested":    {ir.KindUnion, map[string]ir.UnmodeledReason{}},
			"HeldUnion": {ir.KindScalar, map[string]ir.UnmodeledReason{"openapi:oneOf": ir.ReasonDegradedLowering}},
			"BareRequired": {ir.KindScalar, map[string]ir.UnmodeledReason{
				"openapi:oneOf": ir.ReasonDegradedLowering, "openapi:required": ir.ReasonDegradedLowering}},
			"Entry":    {ir.KindUnion, map[string]ir.UnmodeledReason{}},
			"RefEntry": {ir.KindUnion, map[string]ir.UnmodeledReason{}},
		},
	}
	for _, off := range []bool{false, true} {
		for _, name := range []string{"Closed", "Required", "Typed"} {
			want[off][name] = outcome{ir.KindModel, map[string]ir.UnmodeledReason{"openapi:oneOf": ir.ReasonValidationOnly}}
		}
	}
	for _, version := range []string{"3.0.3", "3.1.0"} {
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			inBothOrders(t, schemas, func(t *testing.T, schemas []string) {
				for _, off := range []bool{false, true} {
					doc, diags := compileKindInference(t, "says", admissionSpec(version, "", schemas...), off)
					requireErrorsAndWarnings(t, diags, nil)
					for name, outcome := range want[off] {
						td := kindNode(t, doc, componentID(name))
						assert.Equal(t, outcome.kind, td.Kind(), "%s off=%t", name, off)
						assert.Equal(t, outcome.kept, keptOn(td), "%s off=%t", name, off)
					}
				}
			})
		})
	}
}
