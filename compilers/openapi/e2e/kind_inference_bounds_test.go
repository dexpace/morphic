// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// boundedShape is a schema naming no kind beside bounds, some its inferred
// kind takes, with what its node holds and keeps under each policy.
type boundedShape struct {
	key, schema string
	on, off     boundedNode
}

// boundedNode is a node's kind, the bounds its Constraints hold as encoded,
// the keys it keeps, and, for a union, each variant's kind and bounds.
type boundedNode struct {
	kind     ir.TypeKind
	bounds   map[string]any
	kept     []string
	variants []boundedNode
}

// boundedShapes are an object, an array, a map and a union of an object and an
// array, each beside a bound of its kind and one of another.
var boundedShapes = []boundedShape{
	{"P", "{properties: {a: {type: string}}, minProperties: 1, minLength: 2}",
		boundedNode{kind: ir.KindModel, bounds: map[string]any{"minProperties": 1.0}, kept: []string{"minLength"}},
		boundedNode{kind: ir.KindScalar, bounds: map[string]any{"minProperties": 1.0, "minLength": 2.0}, kept: []string{"properties"}}},
	{"I", "{items: {type: string}, minItems: 1, minProperties: 1}",
		boundedNode{kind: ir.KindList, bounds: map[string]any{"minItems": 1.0}, kept: []string{"minProperties"}},
		boundedNode{kind: ir.KindScalar, bounds: map[string]any{"minProperties": 1.0}, kept: []string{"items", "minItems"}}},
	{"A", "{additionalProperties: {type: string}, maxProperties: 3, maximum: 5}",
		boundedNode{kind: ir.KindMap, bounds: map[string]any{"maxProperties": 3.0}, kept: []string{"maximum"}},
		boundedNode{kind: ir.KindScalar, bounds: map[string]any{"maxProperties": 3.0, "max": "5"}, kept: []string{"additionalProperties"}}},
	{"U", "{properties: {a: {type: string}}, items: {type: string}, minItems: 1, minProperties: 1, minLength: 1}",
		boundedNode{kind: ir.KindUnion, bounds: map[string]any{}, kept: []string{"minLength"}, variants: []boundedNode{
			{kind: ir.KindModel, bounds: map[string]any{"minProperties": 1.0}},
			{kind: ir.KindList, bounds: map[string]any{"minItems": 1.0}},
		}},
		boundedNode{kind: ir.KindScalar, bounds: map[string]any{"minProperties": 1.0, "minLength": 1.0},
			kept: []string{"items", "minItems", "properties"}}},
}

// boundedSpec writes each bounded shape as a component, a property, a query
// parameter and a response header, the components in the order given.
func boundedSpec(version string, components []string) string {
	var params, headers, properties strings.Builder
	for _, sh := range boundedShapes {
		fmt.Fprintf(&params, "        - {name: q%s, in: query, schema: %s}\n", sh.key, sh.schema)
		fmt.Fprintf(&headers, "            H%s: {schema: %s}\n", sh.key, sh.schema)
		fmt.Fprintf(&properties, "        p%s: %s\n", sh.key, sh.schema)
	}
	paths := "paths:\n  /x:\n    get:\n      operationId: get\n      parameters:\n" + params.String() +
		"      responses:\n        '200':\n          description: ok\n          headers:\n" + headers.String()
	holder := "Holder:\n      type: object\n      properties:\n" + strings.TrimSuffix(properties.String(), "\n")
	return admissionSpec(version, paths, slices.Concat(components, []string{holder})...)
}

// encodedBounds is what v's encoded Constraints bound, the uniqueness flag
// every one encodes left out.
func encodedBounds(t *testing.T, v any) map[string]any {
	t.Helper()
	raw, err := json.Marshal(v)
	require.NoError(t, err)
	var tree map[string]any
	require.NoError(t, json.Unmarshal(raw, &tree))
	bounds, _ := tree["constraints"].(map[string]any)
	if bounds == nil {
		bounds = map[string]any{}
	}
	delete(bounds, "uniqueItems")
	return bounds
}

// assertBoundedNode holds the node at id to want, a union's variants in order.
func assertBoundedNode(t *testing.T, doc *ir.Document, id ir.TypeID, want boundedNode) {
	t.Helper()
	td := kindNode(t, doc, id)
	require.Equal(t, want.kind, td.Kind(), id)
	assert.Equal(t, want.bounds, encodedBounds(t, td), "%s holds each bound its kind takes, once", id)
	kept := map[string]ir.UnmodeledReason{}
	for _, keyword := range want.kept {
		kept["openapi:"+keyword] = ir.ReasonDegradedLowering
	}
	assert.Equal(t, kept, keptOn(td), "%s keeps each bound no node of it takes, once", id)
	if len(want.variants) == 0 {
		return
	}
	union, ok := td.(*ir.Union)
	require.True(t, ok, id)
	require.Len(t, union.Variants, len(want.variants), id)
	for i, variant := range union.Variants {
		assertBoundedNode(t, doc, variant.Type.Target, want.variants[i])
	}
}

// TestKindInference_ABoundLandsOnTheInferredNodeOfItsKind holds a bound beside
// a schema naming no kind to the node the position owns: with the policy on,
// the inferred node, or the variant of an inferred union, whose kind takes it,
// and kept once on the node where none does; with the policy off, the top type,
// which takes every value bound and keeps a collection count. Each position
// draws one info, and no carrier holds a bound of the node it owns.
func TestKindInference_ABoundLandsOnTheInferredNodeOfItsKind(t *testing.T) {
	t.Parallel()
	components := make([]string, 0, len(boundedShapes))
	for _, sh := range boundedShapes {
		components = append(components, sh.key+": "+sh.schema)
	}
	for _, version := range []string{"3.0.3", "3.1.0"} {
		for _, off := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/off=%t", version, off), func(t *testing.T) {
				t.Parallel()
				inBothOrders(t, components, func(t *testing.T, components []string) {
					doc, diags := compileKindInference(t, "bounded", boundedSpec(version, components), off)
					infos := degradedInfoPointers(diags)
					for i, sh := range boundedShapes {
						want := sh.on
						if off {
							want = sh.off
						}
						for pointer, id := range map[string]ir.TypeID{
							"/components/schemas/" + sh.key:                       componentID(sh.key),
							"/components/schemas/Holder/properties/p" + sh.key:    ir.TypeID("type/anonymous/components/schemas/Holder/properties/p" + sh.key),
							fmt.Sprintf("/paths/~1x/get/parameters/%d/schema", i): ir.TypeID(fmt.Sprintf("type/anonymous/paths/~1x/get/parameters/%d/schema", i)),
							"/paths/~1x/get/responses/200/headers/H" + sh.key + "/schema": ir.TypeID(
								"type/anonymous/paths/~1x/get/responses/200/headers/H" + sh.key + "/schema"),
						} {
							assertBoundedNode(t, doc, id, want)
							assert.Equal(t, 1, countOf(infos, jsontext.Pointer(pointer)), "one info at %s", pointer)
						}
						assert.Empty(t, encodedBounds(t, holderProperties(t, doc)["p"+sh.key]), "the property holds none")
					}
					operation := doc.Services[0].Groups[0].Operations[0]
					for _, parameter := range operation.Params {
						assert.Empty(t, encodedBounds(t, parameter), "parameter %s holds none", parameter.Name.Source)
					}
					for _, response := range operation.Responses {
						for _, header := range response.Headers {
							assert.Empty(t, encodedBounds(t, header), "header %s holds none", header.Name.Source)
						}
					}
				})
			})
		}
	}
}
