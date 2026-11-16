// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

//go:build pending

package e2e_test

import (
	"encoding/json/v2"
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
	{"ReqOnly: {required: [a]}", "ReqTyped: {type: object, required: [a]}", "ReqOnly", "ReqTyped", ir.KindModel, false},
	{"DiscOnly: {discriminator: {propertyName: k}}", "DiscTyped: {type: object, discriminator: {propertyName: k}}", "DiscOnly", "DiscTyped", ir.KindModel, false},
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

// TestKindInference_EveryShapeKeywordInfersTheKindItSpeaksOf holds that a
// schema writing one shape keyword and no type lowers as the schema writing the
// type that keyword speaks of does, and carries the marker the twin lacks.
// The keywords are items, additionalProperties, required, discriminator,
// patternProperties and prefixItems; the last two are 3.1 keywords.
func TestKindInference_EveryShapeKeywordInfersTheKindItSpeaksOf(t *testing.T) {
	t.Parallel()
	for _, version := range []string{"3.0.3", "3.1.0"} {
		var schemas []string
		for _, tc := range shapeTwins {
			if !(tc.only31 && version == "3.0.3") {
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
// kind read off a keyword carries what the keyword says: the element, value
// and required names the three schemas write are in the nodes the policy makes.
func TestKindInference_AnInferredCollectionKeepsItsMemberTypes(t *testing.T) {
	t.Parallel()
	schemas := []string{
		"Elements: {items: {type: string}}",
		"AddlOnly: {additionalProperties: {type: string}}",
		"ReqOnly: {required: [a]}",
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
				model, ok := kindNode(t, doc, componentID("ReqOnly")).(*ir.Model)
				require.True(t, ok)
				assert.Equal(t, []string{"a"}, model.Requires)
			})
		})
	}
}

// TestKindInference_AnInlineCollectionIsMarkedAtItsPosition holds that the
// marker stands on the list, map or model an inline schema owns at a property,
// for the keywords other than `properties`.
func TestKindInference_AnInlineCollectionIsMarkedAtItsPosition(t *testing.T) {
	t.Parallel()
	holder := "Holder: {type: object, properties: {it: {items: {type: string}}, " +
		"ad: {additionalProperties: {type: string}}, rq: {required: [a]}}}"
	want := map[ir.TypeID]ir.TypeKind{
		"t/anon/components/schemas/Holder/properties/it": ir.KindList,
		"t/anon/components/schemas/Holder/properties/ad": ir.KindMap,
		"t/anon/components/schemas/Holder/properties/rq": ir.KindModel,
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
		{"ReqItems", "required: [a], items: {type: string}", "ReqItemsT"},
		{"AddlItems", "additionalProperties: {type: string}, items: {type: string}", "AddlItemsT"},
	}
	var schemas []string
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
