// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// TestElection_AUnionAndTheWordsItTiesWithAreTheUnionHoldingTheBound holds
// that an allOf of a union and an entry refining it lowers to the union,
// standing at the position whichever is written first: the branch's bound is
// held once, in Union.Constraints, at a property and at a component, no entry
// is kept and nothing is reported (docs/compilers/openapi.md#allof-refinement).
func TestElection_AUnionAndTheWordsItTiesWithAreTheUnionHoldingTheBound(t *testing.T) {
	t.Parallel()
	for _, version := range electionVersions {
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			doc, diags := compileInEveryOrder(t, version, func(swapped bool) []string {
				list, _, _ := composeEntries(swapped,
					"{oneOf: [{$ref: '#/components/schemas/SimpleString'}]}", "{type: string, minLength: 1}")
				return []string{
					"SimpleString: {type: string}",
					"N: {type: object, properties: {p: {allOf: " + list + "}}}",
					"C: {allOf: " + list + "}",
				}
			})
			requireErrorsAndWarnings(t, diags, nil)
			assert.Zero(t, degradedInfosAt(diags, "N"))
			assert.Zero(t, degradedInfosAt(diags, "C"))

			property := requireModelOf(t, doc, "N").Properties[0]
			assert.Equal(t, ir.TypeID("type/anonymous/components/schemas/N/properties/p"), property.Type.Target, "the union stands at the property")
			for _, id := range []ir.TypeID{property.Type.Target, componentID("C")} {
				union, ok := kindNode(t, doc, id).(*ir.Union)
				require.True(t, ok, "%s is the union, not the string beside it", id)
				require.NotNil(t, union.Constraints, id)
				require.NotNil(t, union.Constraints.MinLength, id)
				assert.EqualValues(t, 1, *union.Constraints.MinLength, "%s holds the bound", id)
				assert.Empty(t, keptOn(union), id)
			}
			assert.Nil(t, property.Constraints, "the property holds none")
			assert.Empty(t, property.Unmodeled, "the words are held, not kept")
		})
	}
}
