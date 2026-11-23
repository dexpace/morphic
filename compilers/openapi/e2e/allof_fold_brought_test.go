// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// memberF is the property f of the model id names.
func memberF(t *testing.T, doc *ir.Document, id ir.TypeID) ir.Property {
	t.Helper()
	model, isModel := doc.Types[id].(*ir.Model)
	require.True(t, isModel, "%s is a model", id)
	property, ok := propertyByWire(model, "f")
	require.True(t, ok, "%s holds f", id)
	return property
}

// TestAllOfFold_ADeprecationIsEachMembersOwn pins that a deprecation a fold
// takes is the folded member's own copy: a message naming a member beside it is
// read into each model's by that model's members, whichever kind the member's
// type is and whichever model sorts first, so a base two models compose holds
// its own and each of theirs names its own.
func TestAllOfFold_ADeprecationIsEachMembersOwn(t *testing.T) {
	t.Parallel()
	const reason = ", deprecated: true, x-deprecated-reason: 'Use `g` instead.'"
	for _, kind := range []string{"type: array, items: {type: string}", "type: object, additionalProperties: {type: string}", "type: string, enum: [a, b]",
		"type: object, properties: {p: {type: string}}", "anyOf: [{type: string}, {type: integer}]", "type: string"} {
		for _, names := range [][2]string{{"Cat", "Dog"}, {"Dog", "Cat"}, {"Aa", "Zz"}} {
			src := "    Base: {properties: {f: {" + kind + reason + "}, g: {type: array, items: {type: string}}}}\n"
			for i, name := range names {
				src += "    " + name + ": {allOf: [{$ref: '#/components/schemas/Base'}, {properties: {f: {" + kind +
					fmt.Sprintf("}, g: {type: array, items: {type: string}, maxItems: %d}}}]}\n", 5+i)
			}
			doc, _ := parseVerified(t, openapitest.ComponentSpec(src))
			for _, name := range []string{"Base", names[0], names[1]} {
				model, isModel := doc.Types[componentID(name)].(*ir.Model)
				require.True(t, isModel, name)
				sibling, ok := propertyByWire(model, "g")
				require.True(t, ok, name)
				deprecation := memberF(t, doc, componentID(name)).Deprecation
				require.NotNil(t, deprecation, "%s: %s", kind, name)
				assert.Equal(t, []ir.EntityRef{{Property: sibling.ID}}, deprecation.ReplacedBy, "%s, %v: %s", kind, names, name)
			}
		}
	}

	// An object's members an inline composition declares twice: the first
	// object's names its own sibling, not the composed model's.
	doc, _ := parseVerified(t, openapitest.ComponentSpec("    M: {allOf: ["+
		"{properties: {f: {type: object, properties: {f: {type: array, items: {type: string}"+reason+"}, g: {type: array, items: {type: string}}}}}}, "+
		"{properties: {f: {type: object, properties: {f: {type: array, items: {type: string}}, g: {type: array, items: {type: string}, maxItems: 5}}}}}]}\n"))
	const first = "components/schemas/M/allOf/0/properties/f"
	assert.Equal(t, []ir.EntityRef{{Property: "property/openapi/" + first + "/properties/g"}},
		memberF(t, doc, "type/anonymous/"+first).Deprecation.ReplacedBy)
}

// TestAllOfFold_ABuiltNodesDeprecationIsItsOwn pins the node a fold builds of a
// deprecation one declaration's node holds and a replacement key another's
// does: the built node resolves the key it keeps, and the node it took the
// deprecation from its own.
func TestAllOfFold_ABuiltNodesDeprecationIsItsOwn(t *testing.T) {
	t.Parallel()
	src := openapitest.ComponentSpec("    New: {type: string}\n    Newer: {type: string}\n    M: {allOf: [" +
		"{properties: {f: {type: string, enum: [a, b], deprecated: true, x-replaced-by: New}}}, " +
		"{properties: {f: {type: string, enum: [a, c], x-replaced-by: Newer}}}, " +
		"{properties: {f: {type: string, enum: [a, d]}}}]}\n")
	doc, _ := compileVerified(t, openapitest.SourceOf(src), openapi.Options{Promotions: openapi.ExtensionPromotions{
		Targets: map[openapi.ExtensionTarget][]openapi.ExtensionSource{openapi.TargetDeprecationReplacedBy: {{Key: "x-replaced-by"}}},
	}})

	built := doc.Types[memberF(t, doc, componentID("M")).Type.Target].Common()
	require.NotNil(t, built.Deprecation)
	assert.Equal(t, []ir.EntityRef{{Type: componentID("Newer")}}, built.Deprecation.ReplacedBy)
	written := doc.Types["type/anonymous/components/schemas/M/allOf/0/properties/f"].Common()
	require.NotNil(t, written.Deprecation)
	assert.Equal(t, []ir.EntityRef{{Type: componentID("New")}}, written.Deprecation.ReplacedBy)
}
