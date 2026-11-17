// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// TestElection_AUnionAndTheWordsItTiesWithAreAUseOfTheUnionHoldingTheBound
// holds that an allOf of a union and a branch refining it lowers to a use of
// the union, built where its entry is written, whichever is written first: the
// branch's bound is held once by the use, the property at a property and the
// alias over the union at a component, no entry is kept and nothing is
// reported. Union.Constraints holds none, as the union stands at its entry
// (docs/compilers/openapi.md#allof-refinement).
func TestElection_AUnionAndTheWordsItTiesWithAreAUseOfTheUnionHoldingTheBound(t *testing.T) {
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

			p := requireModelOf(t, doc, "N").Properties[0]
			union, ok := kindNode(t, doc, p.Type.Target).(*ir.Union)
			require.True(t, ok, "the property names the union, not the string beside it")
			require.NotNil(t, p.Constraints)
			require.NotNil(t, p.Constraints.MinLength)
			assert.EqualValues(t, 1, *p.Constraints.MinLength, "the property holds the bound")
			assert.Nil(t, union.Constraints, "the union at its entry holds none")
			assert.Empty(t, keptOn(union))
			assert.Empty(t, p.Unmodeled, "the words are held, not kept")

			alias, ok := kindNode(t, doc, componentID("C")).(*ir.Scalar)
			require.True(t, ok, "the component is a use of the union")
			require.NotNil(t, alias.Base)
			_, overUnion := kindNode(t, doc, alias.Base.Target).(*ir.Union)
			assert.True(t, overUnion, "over the union")
			require.NotNil(t, alias.Constraints)
			require.NotNil(t, alias.Constraints.MinLength)
			assert.EqualValues(t, 1, *alias.Constraints.MinLength, "the alias holds the bound")
			assert.Empty(t, keptOn(alias))
		})
	}
}
