// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irverify"
)

// homeRow is one schema written at a carrier, a property, a parameter or a
// header, and where its bounds are held: the carrier's own, the kind of the
// node its type names, and that node's.
type homeRow struct {
	name    string
	schema  string
	kind    string
	carrier map[string]any
	node    map[string]any
	// sibling names the keyword written beside a $ref, which OpenAPI 3.0
	// ignores there and the compiler warns of.
	sibling string
}

// position is a kind of carrier: how it writes the schemas of rows, the ID of
// the carrier a row names and the pointer of the schema it writes.
type position struct {
	name      string
	spec      func(version string, rows []homeRow, schemas []string) string
	carrierID func(row homeRow) string
	schemaAt  func(i int, row homeRow) string
}

const (
	holderPropertiesAt = "/components/schemas/Holder/properties/"
	boundOperationAt   = "/paths/~1a/get"
)

// positions are the three places a schema is written beside the carrier that
// names it. The rows written at each are the same, so a fact that holds of one
// is asked of the others.
var positions = []position{
	{
		name: "property",
		spec: func(version string, rows []homeRow, schemas []string) string {
			properties := make([]string, 0, len(rows))
			for _, homeRow := range rows {
				properties = append(properties, homeRow.name+": "+homeRow.schema)
			}
			holder := "Holder: {type: object, properties: {" + strings.Join(properties, ", ") + "}}"
			// Between the shared schemas, so the reversed order puts it on the
			// other side of each.
			return admissionSpec(version, "", slices.Concat(schemas[:1], []string{holder}, schemas[1:])...)
		},
		carrierID: func(homeRowValue homeRow) string {
			return "property/openapi/components/schemas/Holder/properties/" + homeRowValue.name
		},
		schemaAt: func(_ int, homeRowValue homeRow) string { return holderPropertiesAt + homeRowValue.name },
	},
	{
		name: "parameter",
		spec: func(version string, rows []homeRow, schemas []string) string {
			var b strings.Builder
			b.WriteString("paths:\n  /a:\n    get:\n      parameters:\n")
			for _, homeRow := range rows {
				fmt.Fprintf(&b, "        - {name: %s, in: query, schema: %s}\n", homeRow.name, homeRow.schema)
			}
			b.WriteString("      responses:\n        '200': {description: ok}\n")
			return admissionSpec(version, b.String(), schemas...)
		},
		carrierID: func(homeRowValue homeRow) string {
			return "parameter/openapi/paths/~1a/get/parameters/" + homeRowValue.name + "/query"
		},
		schemaAt: func(i int, _ homeRow) string { return fmt.Sprintf("%s/parameters/%d/schema", boundOperationAt, i) },
	},
	{
		name: "header",
		spec: func(version string, rows []homeRow, schemas []string) string {
			var b strings.Builder
			b.WriteString("paths:\n  /a:\n    get:\n      responses:\n        '200':\n          description: ok\n          headers:\n")
			for _, homeRow := range rows {
				fmt.Fprintf(&b, "            %s: {schema: %s}\n", homeRow.name, homeRow.schema)
			}
			return admissionSpec(version, b.String(), schemas...)
		},
		carrierID: func(homeRowValue homeRow) string {
			return "property/openapi/paths/~1a/get/responses/200/headers/" + homeRowValue.name
		},
		schemaAt: func(_ int, homeRowValue homeRow) string {
			return boundOperationAt + "/responses/200/headers/" + homeRowValue.name + "/schema"
		},
	},
}

// sharedSchemas are the components every row may name.
var sharedSchemas = []string{
	"Str: {type: string}",
	"Obj: {type: object, properties: {a: {type: string}}}",
}

// placement is a carrier as the document holds it and the node its type
// names, nil where the type is a primitive every use shares.
type placement struct {
	carrier map[string]any
	typeID  string
	node    map[string]any
}

// placementOf finds the property, parameter or header with ID carrierID in doc,
// read as the JSON encodes it, and the type it names.
func placementOf(t *testing.T, doc *ir.Document, carrierID string) placement {
	t.Helper()
	tree, ok := documentTree(t, doc).(map[string]any)
	require.True(t, ok)
	var carrier map[string]any
	walkObjects(tree, func(m map[string]any) {
		if id, _ := m["id"].(string); id == carrierID {
			carrier = m
		}
	})
	require.NotNil(t, carrier, "the document holds no carrier %s", carrierID)
	ref, ok := carrier["type"].(map[string]any)
	require.True(t, ok, "the carrier %s names a type", carrierID)
	typeID, _ := ref["target"].(string)
	types, _ := tree["types"].(map[string]any)
	node, _ := types[typeID].(map[string]any)
	return placement{carrier: carrier, typeID: typeID, node: node}
}

// heldBounds is the bounds m's constraints hold, without the members a false
// or null value leaves unset.
func heldBounds(m map[string]any) map[string]any {
	held := map[string]any{}
	bounds, _ := m["constraints"].(map[string]any)
	for key, v := range bounds {
		if v != nil && v != false {
			held[key] = v
		}
	}
	return held
}

// kindOf is the kind of the node a placement names, "primitive" where the
// type is one no use owns.
func (p placement) kindOf() string {
	if p.node == nil {
		return "primitive"
	}
	kind, _ := p.node["kind"].(string)
	return kind
}

// requireHome holds both sides of a row: the bounds on the carrier and the
// bounds on its node, so a test cannot pass with a bound on both or on neither.
func requireHome(t *testing.T, placementValue placement, row homeRow) {
	t.Helper()
	assert.Equal(t, row.kind, placementValue.kindOf(), "the kind of the node %s names", placementValue.typeID)
	assert.Equal(t, row.carrier, heldBounds(placementValue.carrier), "the bounds the carrier holds")
	assert.Equal(t, row.node, heldBounds(placementValue.node), "the bounds the node holds")
}

// requireNoNodeAt fails t where doc holds an anonymous type minted at pointer
// or below it: a carrier over a reference owns no node.
func requireNoNodeAt(t *testing.T, doc *ir.Document, pointer string) {
	t.Helper()
	prefix := "type/anonymous" + pointer
	for id := range doc.Types {
		assert.False(t, id == ir.TypeID(prefix) || strings.HasPrefix(string(id), prefix+"/"),
			"%s mints no node for a carrier over a reference", id)
	}
}

// warningsOf is the findings a row draws at version: OpenAPI 3.0 ignores a
// keyword beside a $ref, and the compiler warns that it reads it as 3.1 does.
func warningsOf(version string, pos position, rows []homeRow) []wantFinding {
	if version != "3.0.3" {
		return nil
	}
	var want []wantFinding
	for i, homeRow := range rows {
		if homeRow.sibling != "" {
			want = append(want, wantFinding{
				code: diag.LaterVersionConstruct, pointer: pos.schemaAt(i, homeRow) + "/" + homeRow.sibling,
			})
		}
	}
	return want
}

// keepRows are schemas whose carrier owns no node, because each lowers to a
// reference or to a primitive every use shares.
var keepRows = []homeRow{
	{
		name: "ref", schema: "{$ref: '#/components/schemas/Str', minLength: 2}", kind: "scalar",
		carrier: map[string]any{"minLength": 2.0}, node: map[string]any{}, sibling: "minLength",
	},
	{
		name: "prim", schema: "{type: string, maxLength: 9}", kind: "primitive",
		carrier: map[string]any{"maxLength": 9.0}, node: map[string]any{},
	},
	{
		name: "uuid", schema: "{type: string, format: uuid, maxLength: 36}", kind: "primitive",
		carrier: map[string]any{"maxLength": 36.0}, node: map[string]any{},
	},
}

// TestCompile_ACarrierOverAReferenceOrASharedPrimitiveKeepsItsBound holds that a
// property, a parameter or a header whose schema is a reference with a keyword
// beside it, or a string every use shares, keeps its bound on itself: it owns
// no node to hold it, and no node is minted at its pointer. It holds at both
// versions and in both orders of the components.
func TestCompile_ACarrierOverAReferenceOrASharedPrimitiveKeepsItsBound(t *testing.T) {
	t.Parallel()
	for _, version := range []string{"3.1.0", "3.0.3"} {
		for _, pos := range positions {
			t.Run(version+"/"+pos.name, func(t *testing.T) {
				t.Parallel()
				inBothOrders(t, sharedSchemas, func(t *testing.T, schemas []string) {
					doc, diags := compileAnnotationSpec(t, "keeps", pos.spec(version, keepRows, schemas))
					for i, row := range keepRows {
						placement := placementOf(t, doc, pos.carrierID(row))
						requireHome(t, placement, row)
						requireNoNodeAt(t, doc, pos.schemaAt(i, row))
					}
					requireErrorsAndWarnings(t, diags, warningsOf(version, pos, keepRows))
				})
			})
		}
	}
}

// TestCompile_ABoundBesideAReferenceToAModelIsHeldOnce holds that a property
// count written beside a $ref to a model is held once, by the carrier or by a
// node standing for the use, never by both and never lost: a reader asking for
// the effective bound reads each home once.
func TestCompile_ABoundBesideAReferenceToAModelIsHeldOnce(t *testing.T) {
	t.Parallel()
	row := homeRow{name: "beside", schema: "{$ref: '#/components/schemas/Obj', minProperties: 2}", sibling: "minProperties"}
	for _, version := range []string{"3.1.0", "3.0.3"} {
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			inBothOrders(t, sharedSchemas, func(t *testing.T, schemas []string) {
				pos := positions[0]
				doc, diags := compileAnnotationSpec(t, "beside", pos.spec(version, []homeRow{row}, schemas))
				placement := placementOf(t, doc, pos.carrierID(row))
				carrier, node := heldBounds(placement.carrier), heldBounds(placement.node)
				held := 0
				for _, side := range []map[string]any{carrier, node} {
					if v, ok := side["minProperties"]; ok {
						assert.Equal(t, 2.0, v, "the bound as written")
						held++
					}
				}
				assert.Equal(t, 1, held, "one home holds the bound: carrier %v, node %v", carrier, node)
				requireErrorsAndWarnings(t, diags, warningsOf(version, pos, []homeRow{row}))
			})
		})
	}
}

// TestCompile_AMemberTheRedeclaredPropertyFoldComposesKeepsItsBounds holds that
// a property declared by a base and again by an inline entry, whose type the
// fold composes into a node of its own, the base's holding an XML name the
// entry's does not, keeps the folded bounds on the property while the composed
// node holds none: the fold has no one position to give the bound to. It holds in both orders of the components and of the
// entries, at both versions.
func TestCompile_AMemberTheRedeclaredPropertyFoldComposesKeepsItsBounds(t *testing.T) {
	t.Parallel()
	const inline = "{properties: {action_parameters: {type: object, minProperties: 1, maxProperties: 5, " +
		"properties: {x: {type: string}}}}}"
	for _, version := range []string{"3.1.0", "3.0.3"} {
		for _, swapped := range []bool{false, true} {
			entries := "{$ref: '#/components/schemas/Rule'}, " + inline
			if swapped {
				entries = inline + ", {$ref: '#/components/schemas/Rule'}"
			}
			schemas := []string{
				"Rule: {type: object, properties: {action_parameters: {type: object, xml: {name: d}}}}",
				"LogRule: {allOf: [" + entries + "]}",
			}
			t.Run(fmt.Sprintf("%s/entries-swapped-%t", version, swapped), func(t *testing.T) {
				t.Parallel()
				inBothOrders(t, schemas, func(t *testing.T, schemas []string) {
					doc, _ := compileAnnotationSpec(t, "fold", admissionSpec(version, "", schemas...))
					model, ok := doc.Types["type/openapi/components/schemas/LogRule"].(*ir.Model)
					require.True(t, ok)
					require.Len(t, model.Properties, 1)
					property := model.Properties[0]
					require.NotNil(t, property.Constraints, "the property keeps the folded bounds")
					require.NotNil(t, property.Constraints.MinProperties)
					require.NotNil(t, property.Constraints.MaxProperties)
					assert.EqualValues(t, 1, *property.Constraints.MinProperties)
					assert.EqualValues(t, 5, *property.Constraints.MaxProperties)

					composed, ok := doc.Types[property.Type.Target].(*ir.Model)
					require.True(t, ok, "the property names a model")
					assert.True(t, strings.HasPrefix(string(composed.ID), "type/composed/"),
						"%s is the composed node", composed.ID)
					assert.Nil(t, composed.Constraints, "the composed node holds none of them")
				})
			})
		}
	}
}

// TestCompile_ABoundOfAnotherKindIsKeptOnItsHomeAndBreaksNoInvariant holds that
// a string bound beside a schema that lowers to a model, written alone or
// beside a conjunct no object can satisfy, lands in no Constraints and is kept
// verbatim, that one beside an allOf of a schema admitting every kind is held
// by the use of it, and that the compiled document draws no
// bound-of-another-kind violation.
func TestCompile_ABoundOfAnotherKindIsKeptOnItsHomeAndBreaksNoInvariant(t *testing.T) {
	t.Parallel()
	schemas := []string{
		"Base: {type: object, properties: {id: {type: string}}}",
		"O1: {properties: {a: {type: string}}, minLength: 3}",
		"Contradicted: {allOf: [{$ref: '#/components/schemas/Base'}, {type: string, maxLength: 3}]}",
		"Open: {description: declares no type}",
		"Wide: {allOf: [{$ref: '#/components/schemas/Open'}], minLength: 4}",
	}
	for _, version := range []string{"3.1.0", "3.0.3"} {
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			inBothOrders(t, schemas, func(t *testing.T, schemas []string) {
				doc, _ := compileAnnotationSpec(t, "kind", admissionSpec(version, "", schemas...))
				assert.Equal(t, []string{"type/openapi/components/schemas/Wide"}, boundHomes(t, doc, "minLength"),
					"the use of the top type alone holds the length")
				assert.Empty(t, boundHomes(t, doc, "maxLength"), "no node or carrier holds the other length")
				assert.ElementsMatch(t, []string{"/components/schemas/O1/minLength"}, keptEntries(t, doc, "openapi:minLength"))
				for _, violation := range irverify.Verify(doc) {
					assert.NotEqual(t, "ir/bound-of-another-kind", violation.Code, "violation at %s", violation.Path)
				}
			})
		})
	}
}

// ownedRows are schemas that lower to a node their position owns, so the node
// holds the bounds and the carrier none. The last two are the shapes of two
// properties of the golden specification that hold their bounds twice.
var ownedRows = []homeRow{
	{
		name: "object", schema: "{type: object, properties: {a: {type: string}}, minProperties: 2}",
		kind: "model", carrier: map[string]any{}, node: map[string]any{"minProperties": 2.0},
	},
	{
		name: "map", schema: "{type: object, additionalProperties: {type: string}, minProperties: 2}",
		kind: "map", carrier: map[string]any{}, node: map[string]any{"minProperties": 2.0},
	},
	{
		name: "list", schema: "{type: array, items: {type: string}, minItems: 2}",
		kind: "list", carrier: map[string]any{}, node: map[string]any{"minItems": 2.0},
	},
	{
		name: "wrapped", schema: "{allOf: [{$ref: '#/components/schemas/Obj'}], minProperties: 2}",
		kind: "model", carrier: map[string]any{}, node: map[string]any{"minProperties": 2.0},
	},
	{
		name: "entry", schema: "{allOf: [{$ref: '#/components/schemas/Obj'}, {maxProperties: 3}]}",
		kind: "model", carrier: map[string]any{}, node: map[string]any{"maxProperties": 3.0},
	},
	{
		name: "scalar", schema: "{type: string, format: byte, maxLength: 5}",
		kind: "scalar", carrier: map[string]any{}, node: map[string]any{"maxLength": 5.0},
	},
	{
		name: "union", schema: "{oneOf: [{type: string}, {type: integer}], minLength: 1}",
		kind: "union", carrier: map[string]any{}, node: map[string]any{"minLength": 1.0},
	},
	{
		name:   "metadata",
		schema: "{type: object, additionalProperties: {anyOf: [{type: string}, {type: integer}]}, maxProperties: 32}",
		kind:   "map", carrier: map[string]any{}, node: map[string]any{"maxProperties": 32.0},
	},
	{
		name:   "instanceId",
		schema: "{type: string, minimum: 1, maximum: 100, pattern: '^[a-zA-Z0-9_]+$'}",
		kind:   "scalar", carrier: map[string]any{}, node: map[string]any{"pattern": "^[a-zA-Z0-9_]+$"},
	},
}

// requireOwnedRowsHoldOnce compiles each position's spec at each version, in
// both orders of the components, and holds every row to the node and no
// carrier.
func requireOwnedRowsHoldOnce(t *testing.T, pos position) {
	t.Helper()
	for _, version := range []string{"3.1.0", "3.0.3"} {
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			inBothOrders(t, sharedSchemas, func(t *testing.T, schemas []string) {
				doc, diags := compileAnnotationSpec(t, "owned", pos.spec(version, ownedRows, schemas))
				for _, row := range ownedRows {
					t.Run(row.name, func(t *testing.T) {
						requireHome(t, placementOf(t, doc, pos.carrierID(row)), row)
					})
				}
				requireErrorsAndWarnings(t, diags, nil)
			})
		})
	}
}

// TestCompile_APropertyOverANodeOfItsOwnHoldsNoBoundThatNodeHolds holds that a
// property whose schema lowers to a list, a map, a model, a scalar or a union
// of its own leaves its bounds on that node and states none itself, so a bound
// has one home. It holds at both versions and in both orders of the components.
func TestCompile_APropertyOverANodeOfItsOwnHoldsNoBoundThatNodeHolds(t *testing.T) {
	t.Parallel()
	requireOwnedRowsHoldOnce(t, positions[0])
}

// TestCompile_AParameterOverANodeOfItsOwnHoldsNoBoundThatNodeHolds is the
// property's twin for a parameter.
func TestCompile_AParameterOverANodeOfItsOwnHoldsNoBoundThatNodeHolds(t *testing.T) {
	t.Parallel()
	requireOwnedRowsHoldOnce(t, positions[1])
}

// TestCompile_AHeaderOverANodeOfItsOwnHoldsNoBoundThatNodeHolds is the
// property's twin for a response header.
func TestCompile_AHeaderOverANodeOfItsOwnHoldsNoBoundThatNodeHolds(t *testing.T) {
	t.Parallel()
	requireOwnedRowsHoldOnce(t, positions[2])
}

// TestCompile_AReferenceNamingAPropertyReadsTheNodeHoldingItsBound holds that a
// $ref naming a property's own pointer reads the node that holds the bound: the
// property it names holds none, and both reach one node, whichever of the two
// is declared first.
func TestCompile_AReferenceNamingAPropertyReadsTheNodeHoldingItsBound(t *testing.T) {
	t.Parallel()
	named := []string{"object", "map", "wrapped", "scalar", "union"}
	rows := make([]homeRow, 0, len(named))
	for _, row := range ownedRows {
		for _, name := range named {
			if row.name == name {
				rows = append(rows, row)
			}
		}
	}
	require.Len(t, rows, len(named))
	for _, version := range []string{"3.1.0", "3.0.3"} {
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			pos := positions[0]
			user := "User: {type: object, properties: {"
			for i, row := range rows {
				if i > 0 {
					user += ", "
				}
				user += "r_" + row.name + ": {$ref: '#/components/schemas/Holder/properties/" + row.name + "'}"
			}
			user += "}}"
			inBothOrders(t, append([]string{user}, sharedSchemas...), func(t *testing.T, schemas []string) {
				doc, diags := compileAnnotationSpec(t, "reference", pos.spec(version, rows, schemas))
				for _, row := range rows {
					t.Run(row.name, func(t *testing.T) {
						holder := placementOf(t, doc, pos.carrierID(row))
						requireHome(t, holder, row)
						via := placementOf(t, doc, "property/openapi/components/schemas/User/properties/r_"+row.name)
						assert.Equal(t, holder.typeID, via.typeID, "the reference reads the property's own node")
						assert.Empty(t, heldBounds(via.carrier), "the reference's carrier holds none")
					})
				}
				requireErrorsAndWarnings(t, diags, nil)
			})
		})
	}
}

// TestCompile_AReferenceNamingASharedPrimitivePropertyReadsANodeHoistedThere
// holds that a property whose schema is a string every use shares owns no node,
// so its carrier keeps its bound even where a $ref names its pointer: that
// reference reads a scalar hoisted there for it, which holds the bound, and its
// own carrier none. Each reader finds the bound once, whichever of the two is
// declared first.
func TestCompile_AReferenceNamingASharedPrimitivePropertyReadsANodeHoistedThere(t *testing.T) {
	t.Parallel()
	const hoisted = "type/anonymous" + holderPropertiesAt + "inl"
	schemas := []string{
		"Holder: {type: object, properties: {inl: {type: string, minLength: 3}}}",
		"User: {type: object, properties: {r_inl: {$ref: '#/components/schemas/Holder/properties/inl'}}}",
	}
	for _, version := range []string{"3.1.0", "3.0.3"} {
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			inBothOrders(t, schemas, func(t *testing.T, schemas []string) {
				doc, diags := compileAnnotationSpec(t, "shared", admissionSpec(version, "", schemas...))
				inl := placementOf(t, doc, "property/openapi"+holderPropertiesAt+"inl")
				assert.Equal(t, "primitive", inl.kindOf(), "the property names the shared string")
				assert.Equal(t, map[string]any{"minLength": 3.0}, heldBounds(inl.carrier), "the property keeps its bound")
				via := placementOf(t, doc, "property/openapi/components/schemas/User/properties/r_inl")
				assert.Equal(t, hoisted, via.typeID, "the reference reads a node hoisted at the pointer")
				assert.Equal(t, "scalar", via.kindOf())
				assert.Equal(t, map[string]any{"minLength": 3.0}, heldBounds(via.node), "that node holds the bound")
				assert.Empty(t, heldBounds(via.carrier), "the reference's carrier holds none")
				requireErrorsAndWarnings(t, diags, nil)
			})
		})
	}
}
