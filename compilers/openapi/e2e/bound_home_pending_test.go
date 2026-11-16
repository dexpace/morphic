// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

//go:build pending

package e2e_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ownedRows are schemas that lower to a node their position owns, so the node
// holds the bounds and the carrier none. The last two are the shapes of two
// properties of the golden specification that hold their bounds twice.
var ownedRows = []homeRow{
	{
		name: "object", schema: "{type: object, properties: {a: {type: string}}, minProperties: 2}",
		kind: "model", carrier: map[string]any{}, node: map[string]any{"minProps": 2.0},
	},
	{
		name: "map", schema: "{type: object, additionalProperties: {type: string}, minProperties: 2}",
		kind: "map", carrier: map[string]any{}, node: map[string]any{"minProps": 2.0},
	},
	{
		name: "list", schema: "{type: array, items: {type: string}, minItems: 2}",
		kind: "list", carrier: map[string]any{}, node: map[string]any{"minItems": 2.0},
	},
	{
		name: "wrapped", schema: "{allOf: [{$ref: '#/components/schemas/Obj'}], minProperties: 2}",
		kind: "model", carrier: map[string]any{}, node: map[string]any{"minProps": 2.0},
	},
	{
		name: "entry", schema: "{allOf: [{$ref: '#/components/schemas/Obj'}, {maxProperties: 3}]}",
		kind: "model", carrier: map[string]any{}, node: map[string]any{"maxProps": 3.0},
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
		kind:   "map", carrier: map[string]any{}, node: map[string]any{"maxProps": 32.0},
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
						via := placementOf(t, doc, "p/openapi/components/schemas/User/properties/r_"+row.name)
						assert.Equal(t, holder.typeID, via.typeID, "the reference reads the property's own node")
						assert.Empty(t, heldBounds(via.carrier), "the reference's carrier holds none")
					})
				}
				requireErrorsAndWarnings(t, diags, nil)
			})
		})
	}
}
