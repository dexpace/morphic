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

	union, ok := typeByName(doc, "Both").(*ir.Union)
	require.True(t, ok, "the keywords speak of two kinds")
	assert.Equal(t, lowering.KindInferenceHeuristic, union.Provenance.Inferred)
	assert.Empty(t, union.Unmodeled)
	require.Len(t, union.Variants, 2)
	for i, kind := range []ir.TypeKind{ir.KindModel, ir.KindList} {
		typeDef := doc.Types[union.Variants[i].Type.Target]
		require.NotNil(t, typeDef, "variant %d", i)
		assert.Equal(t, kind, typeDef.Kind(), "variant %d", i)
		assert.Equal(t, lowering.KindInferenceHeuristic, typeDef.Common().Provenance.Inferred, "variant %d", i)
	}
}
