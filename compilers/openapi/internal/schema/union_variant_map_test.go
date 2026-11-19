// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/schema"
	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irverify"
)

// TestOneOf_AVariantKeepingItsBranchBesideAMapIsAMap pins the node a variant
// is where it keeps its branch beside a body that is a map alone: the MapT
// such a model is (docs/ir/containers.md), as every other object lowering
// gives, not a Model that irverify refuses as ir/map-as-model.
func TestOneOf_AVariantKeepingItsBranchBesideAMapIsAMap(t *testing.T) {
	t.Parallel()
	doc, diags := schema.LowerSpec(t, openapitest.ComponentSpec(`    B: {type: object, properties: {b: {type: integer}}}
    M:
      type: object
      additionalProperties: {type: string}
      oneOf: [{$ref: '#/components/schemas/B'}, {anyOf: [{type: object}, {type: string}]}]
`))
	openapitest.RequireNoErrorDiags(t, diags)
	for _, violation := range irverify.Verify(doc) {
		assert.NotEqual(t, "ir/map-as-model", violation.Code, violation.Path)
	}

	union, ok := typeByName(doc, "M").(*ir.Union)
	require.True(t, ok)
	require.Len(t, union.Variants, 2)
	assert.IsType(t, &ir.Model{}, doc.Types[union.Variants[0].Type.Target], "the variant over B composes it")
	mapT, ok := doc.Types[union.Variants[1].Type.Target].(*ir.MapT)
	require.True(t, ok, "the variant keeping its branch is a map and nothing else")
	assert.Contains(t, mapT.Unmodeled, "openapi:oneOf/1")
}
