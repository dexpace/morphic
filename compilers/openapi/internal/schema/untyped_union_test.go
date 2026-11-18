// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/schema"
	"github.com/dexpace/morphic/ir"
)

// TestLowerUntyped_KeywordsOfAnObjectAndAnArrayAreAMarkedUnion pins a schema
// writing no type whose shape keywords speak of an object and an array: under
// the kind-inference policy it is the union `type: [object, array]` beside
// them gives, a model then a list, the union and each variant marked as read
// by the policy, and nothing kept.
func TestLowerUntyped_KeywordsOfAnObjectAndAnArrayAreAMarkedUnion(t *testing.T) {
	t.Parallel()
	doc, diags := schema.LowerSpec(t, openapitest.ComponentSpecVer("3.1.0",
		"    Both: {properties: {a: {type: string}}, items: {type: string}}\n"))
	openapitest.RequireNoErrorDiags(t, diags)

	u, ok := typeByName(doc, "Both").(*ir.Union)
	require.True(t, ok, "the keywords speak of two kinds")
	assert.Equal(t, lowering.KindInferenceHeuristic, u.Provenance.Inferred)
	assert.Empty(t, u.Unmodeled)
	require.Len(t, u.Variants, 2)
	for i, kind := range []ir.TypeKind{ir.KindModel, ir.KindList} {
		v := doc.Types[u.Variants[i].Type.Target]
		require.NotNil(t, v, "variant %d", i)
		assert.Equal(t, kind, v.Kind(), "variant %d", i)
		assert.Equal(t, lowering.KindInferenceHeuristic, v.Common().Provenance.Inferred, "variant %d", i)
	}
}
