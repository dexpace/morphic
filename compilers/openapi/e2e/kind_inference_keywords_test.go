// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"encoding/json/v2"
	"fmt"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// shapeTwins pairs each untyped schema that writes one shape keyword with the
// schema that writes the type the keyword speaks of beside the same keyword.
// The twin is what the untyped one must lower to, bar its marker.
var shapeTwins = []struct {
	untyped, twin string
	name, twinOf  string
	kind          ir.TypeKind
	only31        bool
}{
	{"Elements: {items: {type: string}}", "ElTyped: {type: array, items: {type: string}}", "Elements", "ElTyped", ir.KindList, false},
	{"AddlOnly: {additionalProperties: {type: string}}", "AddlTyped: {type: object, additionalProperties: {type: string}}", "AddlOnly", "AddlTyped", ir.KindMap, false},
	{"PatOnly: {patternProperties: {'^x': {type: string}}}", "PatTyped: {type: object, patternProperties: {'^x': {type: string}}}", "PatOnly", "PatTyped", ir.KindModel, true},
	{"PrefixOnly: {prefixItems: [{type: string}]}", "PrefixTyped: {type: array, prefixItems: [{type: string}]}", "PrefixOnly", "PrefixTyped", ir.KindTuple, true},
}

// nodeWithoutNames is the encoded node at id less what a twin cannot share:
// its ID, its name and its provenance. What remains is the shape, the facts
// it holds and the keys it keeps.
func nodeWithoutNames(t *testing.T, doc *ir.Document, id ir.TypeID) map[string]any {
	t.Helper()
	raw, err := json.Marshal(kindNode(t, doc, id))
	require.NoError(t, err)
	var tree map[string]any
	require.NoError(t, json.Unmarshal(raw, &tree))
	delete(tree, "id")
	delete(tree, "name")
	delete(tree, "provenance")
	return tree
}

// TestKindInference_EveryTypingKeywordInfersTheKindItSpeaksOf holds that a
// schema writing one keyword that types a member, an element or a value, and
// no type, lowers as the schema writing the type that keyword speaks of does,
// and carries the marker the twin lacks. The keywords are items,
// additionalProperties, patternProperties and prefixItems; the last two are
// 3.1 keywords.
func TestKindInference_EveryTypingKeywordInfersTheKindItSpeaksOf(t *testing.T) {
	t.Parallel()
	for _, version := range []string{"3.0.3", "3.1.0"} {
		var schemas []string
		for _, tc := range shapeTwins {
			if !tc.only31 || version != "3.0.3" {
				schemas = append(schemas, tc.untyped, tc.twin)
			}
		}
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			inBothOrders(t, schemas, func(t *testing.T, schemas []string) {
				doc, diags := compileKindInference(t, "twins", admissionSpec(version, "", schemas...), false)
				requireErrorsAndWarnings(t, diags, nil)
				assert.Empty(t, degradedInfoPointers(diags), "an inferred kind keeps nothing")
				for _, tc := range shapeTwins {
					if tc.only31 && version == "3.0.3" {
						continue
					}
					untyped := kindNode(t, doc, componentID(tc.name))
					assert.Equal(t, tc.kind, untyped.Kind(), tc.name)
					assert.True(t, markedKindInference(untyped), "%s reads its kind off its keyword", tc.name)
					assert.Equal(t, kindInferenceMarker, untyped.Common().Provenance.Inferred, tc.name)
					assert.False(t, markedKindInference(kindNode(t, doc, componentID(tc.twinOf))), tc.twinOf)
					assert.Equal(t, nodeWithoutNames(t, doc, componentID(tc.twinOf)),
						nodeWithoutNames(t, doc, componentID(tc.name)),
						"%s lowers as %s does, bar its marker", tc.name, tc.twinOf)
				}
			})
		})
	}
}

// TestKindInference_AnInferredCollectionKeepsItsMemberTypes holds that the
// kind read off a keyword carries what the keyword says: the element and the
// value the two schemas write are in the nodes the policy makes.
func TestKindInference_AnInferredCollectionKeepsItsMemberTypes(t *testing.T) {
	t.Parallel()
	schemas := []string{
		"Elements: {items: {type: string}}",
		"AddlOnly: {additionalProperties: {type: string}}",
	}
	for _, version := range []string{"3.0.3", "3.1.0"} {
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			inBothOrders(t, schemas, func(t *testing.T, schemas []string) {
				doc, _ := compileKindInference(t, "members", admissionSpec(version, "", schemas...), false)
				list, ok := kindNode(t, doc, componentID("Elements")).(*ir.List)
				require.True(t, ok)
				assert.Equal(t, prim(ir.PrimString), list.Elem.Target)
				mapped, ok := kindNode(t, doc, componentID("AddlOnly")).(*ir.MapT)
				require.True(t, ok)
				assert.Equal(t, prim(ir.PrimString), mapped.Value.Target)
			})
		})
	}
}

// TestKindInference_AnInlineCollectionIsMarkedAtItsPosition holds that the
// marker stands on the list or map an inline schema owns at a property, for
// the keywords other than `properties`.
func TestKindInference_AnInlineCollectionIsMarkedAtItsPosition(t *testing.T) {
	t.Parallel()
	holder := "Holder: {type: object, properties: {it: {items: {type: string}}, " +
		"ad: {additionalProperties: {type: string}}}}"
	want := map[ir.TypeID]ir.TypeKind{
		"t/anon/components/schemas/Holder/properties/it": ir.KindList,
		"t/anon/components/schemas/Holder/properties/ad": ir.KindMap,
	}
	for _, version := range []string{"3.0.3", "3.1.0"} {
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			inBothOrders(t, []string{holder, "Plain: {type: string}"}, func(t *testing.T, schemas []string) {
				doc, diags := compileKindInference(t, "inline-collections", admissionSpec(version, "", schemas...), false)
				requireErrorsAndWarnings(t, diags, nil)
				assert.Empty(t, degradedInfoPointers(diags))
				for id, kind := range want {
					td := kindNode(t, doc, id)
					assert.Equal(t, kind, td.Kind(), id)
					assert.True(t, markedKindInference(td), id)
				}
			})
		})
	}
}

// unionShape is a union as a consumer tells it: whether one variant matches,
// and each variant's name hint and node kind in order.
type unionShape struct {
	exclusive bool
	variants  []string
}

// unionShapeOf reads the shape of the union at id.
func unionShapeOf(t *testing.T, doc *ir.Document, id ir.TypeID) unionShape {
	t.Helper()
	union, ok := kindNode(t, doc, id).(*ir.Union)
	require.True(t, ok, "%s is a union", id)
	shape := unionShape{exclusive: union.Exclusive}
	for _, v := range union.Variants {
		shape.variants = append(shape.variants, v.Name.Hint+":"+string(kindNode(t, doc, v.Type.Target).Kind()))
	}
	return shape
}

// TestKindInference_ASchemaSpeakingOfTwoKindsIsAMarkedUnionOfBoth holds that
// keywords of both kinds with no type lower as the same keywords beside
// `type: [object, array]` do: a union of the object's node and the list, with
// the same variants in the same order, marked, nothing kept, nothing reported.
// Neither keyword is a stray one.
func TestKindInference_ASchemaSpeakingOfTwoKindsIsAMarkedUnionOfBoth(t *testing.T) {
	t.Parallel()
	rows := []struct{ name, untyped, typed string }{
		{"Both", "properties: {a: {type: string}}, items: {type: string}", "BothT"},
		{"PatItems", "patternProperties: {'^x': {type: string}}, items: {type: string}", "PatItemsT"},
		{"AddlItems", "additionalProperties: {type: string}, items: {type: string}", "AddlItemsT"},
	}
	schemas := make([]string, 0, 2*len(rows))
	for _, row := range rows {
		schemas = append(schemas,
			row.name+": {"+row.untyped+"}",
			row.typed+": {type: [object, array], "+row.untyped+"}")
	}
	inBothOrders(t, schemas, func(t *testing.T, schemas []string) {
		doc, diags := compileKindInference(t, "two-kinds", admissionSpec("3.1.0", "", schemas...), false)
		requireErrorsAndWarnings(t, diags, nil)
		assert.Empty(t, degradedInfoPointers(diags), "neither keyword is kept")
		for _, row := range rows {
			union := kindNode(t, doc, componentID(row.name))
			assert.Equal(t, ir.KindUnion, union.Kind(), "%s speaks of an object and an array", row.name)
			assert.Equal(t, kindInferenceMarker, union.Common().Provenance.Inferred, row.name)
			assert.Empty(t, keptOn(union), row.name)
			assert.False(t, markedKindInference(kindNode(t, doc, componentID(row.typed))), "%s declares both kinds", row.typed)
			assert.Equal(t, unionShapeOf(t, doc, componentID(row.typed)), unionShapeOf(t, doc, componentID(row.name)),
				"%s is the union %s declares", row.name, row.typed)
		}
	})
}

// TestKindInference_AThreeZeroSchemaSpeakingOfTwoKindsIsAMarkedUnion holds the
// same at 3.0, which has no type list to write the twin with: the union holds
// a model and a list, marked, with nothing kept.
func TestKindInference_AThreeZeroSchemaSpeakingOfTwoKindsIsAMarkedUnion(t *testing.T) {
	t.Parallel()
	schemas := []string{
		"Both: {properties: {a: {type: string}}, items: {type: string}}",
		"Plain: {type: string}",
	}
	inBothOrders(t, schemas, func(t *testing.T, schemas []string) {
		doc, diags := compileKindInference(t, "two-kinds-30", admissionSpec("3.0.3", "", schemas...), false)
		requireErrorsAndWarnings(t, diags, nil)
		assert.Empty(t, degradedInfoPointers(diags))
		union := kindNode(t, doc, componentID("Both"))
		assert.Equal(t, ir.KindUnion, union.Kind())
		assert.Equal(t, kindInferenceMarker, union.Common().Provenance.Inferred)
		assert.Empty(t, keptOn(union))
		assert.ElementsMatch(t, []string{"object:model", "array:list"}, unionShapeOf(t, doc, componentID("Both")).variants)
	})
}

// TestKindInference_EachVariantOfATwoKindUnionIsMarked holds that the variants
// of the union a schema speaking of two kinds lowers to carry the marker as the
// union does: each variant's kind was read off the keywords too, unlike those of
// the union a type array declares.
func TestKindInference_EachVariantOfATwoKindUnionIsMarked(t *testing.T) {
	t.Parallel()
	schemas := []string{
		"Both: {properties: {a: {type: string}}, items: {type: string}}",
		"BothT: {type: [object, array], properties: {a: {type: string}}, items: {type: string}}",
	}
	doc, _ := compileKindInference(t, "variants", admissionSpec("3.1.0", "", schemas...), false)
	for name, marked := range map[string]bool{"Both": true, "BothT": false} {
		union, ok := kindNode(t, doc, componentID(name)).(*ir.Union)
		require.True(t, ok, name)
		require.Len(t, union.Variants, 2, name)
		for _, v := range union.Variants {
			assert.Equal(t, marked, markedKindInference(kindNode(t, doc, v.Type.Target)), "%s: %s", name, v.Type.Target)
		}
	}
}

// TestKindInference_ATwoKindUnionIsOneInEitherKeywordOrder holds that the
// union a schema speaking of two kinds lowers to reads no order off its
// keywords: written object keyword first or array keyword first, the union,
// its variants in order and the findings are one.
func TestKindInference_ATwoKindUnionIsOneInEitherKeywordOrder(t *testing.T) {
	t.Parallel()
	for _, version := range []string{"3.0.3", "3.1.0"} {
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			objectFirst, objectDiags := compileKindInference(t, "object-first", admissionSpec(version, "",
				"Both: {required: [a], properties: {a: {type: string}}, items: {type: string}}"), false)
			arrayFirst, arrayDiags := compileKindInference(t, "array-first", admissionSpec(version, "",
				"Both: {items: {type: string}, properties: {a: {type: string}}, required: [a]}"), false)
			assert.Equal(t, unionShape{exclusive: true, variants: []string{"object:model", "array:list"}},
				unionShapeOf(t, objectFirst, componentID("Both")))
			assert.Equal(t, nodeWithoutNames(t, objectFirst, componentID("Both")),
				nodeWithoutNames(t, arrayFirst, componentID("Both")))
			union, ok := kindNode(t, objectFirst, componentID("Both")).(*ir.Union)
			require.True(t, ok)
			for _, v := range union.Variants {
				assert.Equal(t, nodeWithoutNames(t, objectFirst, v.Type.Target), nodeWithoutNames(t, arrayFirst, v.Type.Target))
			}
			assert.Equal(t, kindFindings(objectDiags), kindFindings(arrayDiags))
		})
	}
}

// kindFindings lists each finding's code and pointer, sorted.
func kindFindings(diags []ir.Diagnostic) []string {
	out := make([]string, 0, len(diags))
	for _, d := range diags {
		out = append(out, d.Code+" "+string(d.Provenance.Pointer))
	}
	slices.Sort(out)
	return out
}

// TestKindInference_AKindAConjunctNamesIsDeclared holds a schema writing no
// type beside a union kept whole, whose branches name kinds: its shape keywords
// pick the kinds of those they speak of, which the document declared, so the
// node is unmarked and the same under either policy, and the top type where
// they speak of none.
func TestKindInference_AKindAConjunctNamesIsDeclared(t *testing.T) {
	t.Parallel()
	schemas := []string{
		"Obj: {type: object, properties: {a: {type: string}}}",
		"Closed: {oneOf: [{type: 'null'}, {$ref: '#/components/schemas/Obj'}], additionalProperties: false}",
		"Scalars: {oneOf: [{type: 'null'}, {type: string}, {type: integer}], items: {type: string}}",
	}
	for _, off := range []bool{false, true} {
		t.Run(fmt.Sprintf("off=%t", off), func(t *testing.T) {
			t.Parallel()
			inBothOrders(t, schemas, func(t *testing.T, schemas []string) {
				doc, _ := compileKindInference(t, "declared", admissionSpec("3.1.0", "", schemas...), off)
				closed := kindNode(t, doc, componentID("Closed"))
				assert.Equal(t, ir.KindModel, closed.Kind(), "the branches declare an object")
				assert.False(t, markedKindInference(closed), "the document declared the kind")
				assert.Equal(t, map[string]ir.UnmodeledReason{"openapi:oneOf": ir.ReasonDegradedLowering}, keptOn(closed))
				assertUntypedOutcome(t, doc, "Scalars", topKeeping("items", "oneOf"))
			})
		})
	}
}

// TestKindInference_KeywordsNarrowingAKeptUnionNameNoKind holds a schema
// writing no type beside a union kept whole whose branches admit a kind its
// shape keywords do not speak of: the keywords would narrow what the document
// declares, so under either policy the node is the top type keeping them, and
// no instance the union admits is outside it.
func TestKindInference_KeywordsNarrowingAKeptUnionNameNoKind(t *testing.T) {
	t.Parallel()
	schemas := []string{
		"Obj: {type: object, properties: {a: {type: string}}}",
		"Required: {oneOf: [{$ref: '#/components/schemas/Obj'}, {type: string}], required: [a]}",
		"Closed: {anyOf: [{$ref: '#/components/schemas/Obj'}, {type: string}], additionalProperties: false}",
		"Elements: {oneOf: [{type: array, items: {type: integer}}, {type: string}], items: {type: string}}",
		"ThroughABranch: {oneOf: [{properties: {u: {type: string}}}, {type: string}], required: [a]}",
	}
	want := map[string]untypedOutcome{
		"Required":       topKeeping("oneOf", "required"),
		"Closed":         topKeeping("anyOf", "additionalProperties"),
		"Elements":       topKeeping("oneOf", "items"),
		"ThroughABranch": topKeeping("oneOf", "required"),
	}
	for _, off := range []bool{false, true} {
		t.Run(fmt.Sprintf("off=%t", off), func(t *testing.T) {
			t.Parallel()
			inBothOrders(t, schemas, func(t *testing.T, schemas []string) {
				doc, _ := compileKindInference(t, "narrowing", admissionSpec("3.1.0", "", schemas...), off)
				for name, outcome := range want {
					assertUntypedOutcome(t, doc, name, outcome)
				}
			})
		})
	}
}

// TestKindInference_APropertySetBesideAUnionOfOtherKindsIsTheTopTypeWhenOff
// holds that the policy off reads no object off a `properties` beside a union
// naming another kind too: the top type keeps both, where the policy on
// distributes the union across the object it reads.
func TestKindInference_APropertySetBesideAUnionOfOtherKindsIsTheTopTypeWhenOff(t *testing.T) {
	t.Parallel()
	schemas := []string{
		"Obj: {type: object, properties: {a: {type: string}}}",
		"Item: {oneOf: [{$ref: '#/components/schemas/Obj'}, {type: string}], properties: {q: {type: integer}}}",
	}
	inBothOrders(t, schemas, func(t *testing.T, schemas []string) {
		off, _ := compileKindInference(t, "off", admissionSpec("3.1.0", "", schemas...), true)
		assertUntypedOutcome(t, off, "Item", topKeeping("oneOf", "properties"))
		on, _ := compileKindInference(t, "on", admissionSpec("3.1.0", "", schemas...), false)
		assert.Equal(t, ir.KindUnion, kindNode(t, on, componentID("Item")).Kind())
	})
}

// TestKindInference_AKindReadThroughAnInferredBranchIsMarked holds a schema
// whose shape keywords speak of the one kind its union's branches admit, where
// the branches name that kind only by the policy's reading of shape keywords:
// their own, a $ref target's or the target a $dynamicRef expands to. The
// node's kind rests on the policy, so it is marked, and with the policy off it
// is the top type.
func TestKindInference_AKindReadThroughAnInferredBranchIsMarked(t *testing.T) {
	t.Parallel()
	schemas := []string{
		"Props: {$dynamicAnchor: node, properties: {a: {type: string}}}",
		"Inline: {properties: {a: {type: string}, b: {type: string}}, anyOf: [{required: [a]}, {required: [b]}]}",
		"ByRef: {oneOf: [{$ref: '#/components/schemas/Props'}], additionalProperties: false}",
		"ByDynamicRef: {oneOf: [{$dynamicRef: '#node'}], additionalProperties: false}",
	}
	inBothOrders(t, schemas, func(t *testing.T, schemas []string) {
		on, _ := compileKindInference(t, "on", admissionSpec("3.1.0", "", schemas...), false)
		off, _ := compileKindInference(t, "off", admissionSpec("3.1.0", "", schemas...), true)
		for _, name := range []string{"Inline", "ByRef", "ByDynamicRef"} {
			inferred := kindNode(t, on, componentID(name))
			assert.Equal(t, ir.KindModel, inferred.Kind(), name)
			assert.True(t, markedKindInference(inferred), "%s: its branches are objects only as the policy reads them", name)
			scalar, ok := kindNode(t, off, componentID(name)).(*ir.Scalar)
			require.True(t, ok, name)
			require.NotNil(t, scalar.Base, name)
			assert.Equal(t, prim(ir.PrimAny), scalar.Base.Target, name)
		}
	})
}

// TestKindInference_AVariantComposedOverAnInferredBodyIsMarked holds the
// variants a union distributes an untyped property set into: one composed over
// a branch declaring an object is a model by that branch and unmarked, and one
// over a branch that is no object, or an object only as the policy reads it, is
// a model by the policy's reading of the property set and names it, as does one
// over a branch whose read a cycle cuts short, which the policy off composes
// nothing over. A body writing `type: object` marks none.
func TestKindInference_AVariantComposedOverAnInferredBodyIsMarked(t *testing.T) {
	t.Parallel()
	const branches = "oneOf: [{type: object, properties: {o: {type: string}}}, {type: string}, {properties: {u: {type: string}}}]"
	schemas := []string{
		"Untyped: {properties: {x: {type: string}}, " + branches + "}",
		"Typed: {type: object, properties: {x: {type: string}}, " + branches + "}",
		"Cycle: {allOf: [{$ref: '#/components/schemas/Cycle'}]}",
		"CutShort: {properties: {x: {type: string}}, oneOf: [{type: object, allOf: [{$ref: '#/components/schemas/Cycle'}]}]}",
	}
	want := map[string][]bool{"Untyped": {false, true, true}, "Typed": {false, false, false}, "CutShort": {true}}
	inBothOrders(t, schemas, func(t *testing.T, schemas []string) {
		doc, _ := compileKindInference(t, "variants", admissionSpec("3.1.0", "", schemas...), false)
		for name, marked := range want {
			union, ok := kindNode(t, doc, componentID(name)).(*ir.Union)
			require.True(t, ok, name)
			require.Len(t, union.Variants, len(marked), name)
			for i, v := range union.Variants {
				variant := kindNode(t, doc, v.Type.Target)
				assert.Equal(t, ir.KindModel, variant.Kind(), "%s variant %d", name, i)
				assert.Equal(t, marked[i], markedKindInference(variant), "%s variant %d", name, i)
			}
		}
	})
}

// TestKindInference_AModelBesideAKeptUnionIsReportedAsOne holds the report of
// a union kept beside a body that is a model by the kind its branches declare:
// it says the body declares no property, never that the body is no model.
func TestKindInference_AModelBesideAKeptUnionIsReportedAsOne(t *testing.T) {
	t.Parallel()
	schemas := []string{
		"Apple: {type: object, properties: {cultivar: {type: string}}, required: [cultivar]}",
		"Banana: {type: object, properties: {lengthCm: {type: number}}, required: [lengthCm]}",
		"Fruit: {oneOf: [{$ref: '#/components/schemas/Apple'}, {$ref: '#/components/schemas/Banana'}], additionalProperties: false}",
	}
	for _, off := range []bool{false, true} {
		t.Run(fmt.Sprintf("off=%t", off), func(t *testing.T) {
			t.Parallel()
			doc, diags := compileKindInference(t, "fruit", admissionSpec("3.0.3", "", schemas...), off)
			fruit, ok := kindNode(t, doc, componentID("Fruit")).(*ir.Model)
			require.True(t, ok)
			assert.False(t, markedKindInference(fruit), "both branches declare an object")
			message := diagMessageOf(t, diags, "/components/schemas/Fruit")
			assert.Contains(t, message, "the body declares no property")
			assert.NotContains(t, message, "is not a model")
		})
	}
}
