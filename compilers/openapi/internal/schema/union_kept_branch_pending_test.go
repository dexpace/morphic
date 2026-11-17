// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

//go:build pending

package schema_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
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
      oneOf: [{$ref: '#/components/schemas/B'}, {type: string}]
`))
	openapitest.RequireNoErrorDiags(t, diags)
	for _, v := range irverify.Verify(doc) {
		assert.NotEqual(t, "ir/map-as-model", v.Code, v.Path)
	}

	u, ok := typeByName(doc, "M").(*ir.Union)
	require.True(t, ok)
	require.Len(t, u.Variants, 2)
	assert.IsType(t, &ir.Model{}, doc.Types[u.Variants[0].Type.Target], "the variant over B composes it")
	m, ok := doc.Types[u.Variants[1].Type.Target].(*ir.MapT)
	require.True(t, ok, "the variant keeping its branch is a map and nothing else")
	assert.Contains(t, m.Unmodeled, "openapi:oneOf/1")
}

// TestOneOf_AKeptBranchIsHeldOnceAndReportedOnce pins a branch a variant keeps
// verbatim: what it writes is held there alone, not also on a node lowered at
// the branch's pointer that nothing names, and the branch draws one info,
// whether it writes a keyword no node lowered there takes, a bound or a
// keyword beside its $ref.
func TestOneOf_AKeptBranchIsHeldOnceAndReportedOnce(t *testing.T) {
	t.Parallel()
	doc, diags := schema.LowerSpec(t, openapitest.ComponentSpecVer("3.1.0", `    B: {type: object, properties: {b: {type: integer}}}
    TL: {type: [object, string], properties: {t: {type: string}}}
    S:
      type: object
      properties: {p: {type: string}}
      oneOf:
        - $ref: '#/components/schemas/B'
        - {required: [x]}
        - {type: string, minLength: 3}
        - {$ref: '#/components/schemas/TL', minProperties: 2}
`))
	openapitest.RequireNoErrorDiags(t, diags)

	u, ok := typeByName(doc, "S").(*ir.Union)
	require.True(t, ok)
	require.Len(t, u.Variants, 4)
	for i := 1; i < len(u.Variants); i++ {
		v, ok := doc.Types[u.Variants[i].Type.Target].(*ir.Model)
		require.True(t, ok, "variant %d", i)
		assert.Contains(t, v.Unmodeled, fmt.Sprintf("openapi:oneOf/%d", i), "variant %d keeps its branch", i)
		at := fmt.Sprintf("/components/schemas/S/oneOf/%d", i)
		assert.NotContains(t, doc.Types, ir.TypeID("t/anon"+at), "no node nothing names holds branch %d again", i)
		assert.Len(t, diagsAtPointer(diags, diag.DegradedConstruct, at), 1, "one info at %s", at)
	}
}
