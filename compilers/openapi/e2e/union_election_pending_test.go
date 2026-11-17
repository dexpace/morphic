// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

//go:build pending

package e2e_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// TestElection_AUnionAndTheWordsItTiesWithAreTheUnionBoundByTheWords holds that
// an allOf of a union and a branch refining it lowers to the union holding the
// branch's bound, whichever is written first: no entry is kept and nothing is
// reported, where the order used to decide whether the union was typed or
// kept.
func TestElection_AUnionAndTheWordsItTiesWithAreTheUnionBoundByTheWords(t *testing.T) {
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
				}
			})
			requireErrorsAndWarnings(t, diags, nil)
			assert.Zero(t, degradedInfosAt(diags, "N"))

			typed := kindNode(t, doc, propertyTypeOf(t, doc, "N", "p"))
			union, ok := typed.(*ir.Union)
			require.True(t, ok, "the property names the union, not the string beside it: %T", typed)
			require.NotNil(t, union.Constraints)
			require.NotNil(t, union.Constraints.MinLength)
			assert.EqualValues(t, 1, *union.Constraints.MinLength)
			assert.Empty(t, keptOn(union), "the words are held, not kept")
		})
	}
}

// propertyTypeOf is the type the named property of the component names.
func propertyTypeOf(t *testing.T, doc *ir.Document, component, property string) ir.TypeID {
	t.Helper()
	model := requireModelOf(t, doc, component)
	for _, p := range model.Properties {
		if p.Name.Source == property {
			return p.Type.Target
		}
	}
	require.Failf(t, "no such property", "%s holds no property %s", component, property)
	return ""
}
