// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema_test

import (
	"maps"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/schema"
	"github.com/dexpace/morphic/ir"
)

// TestOneOf_AKeptBranchIsKeyedByItsKeywordAndIndex pins the key a variant
// keeps a branch it does not compose under: its union's keyword and its index
// (docs/compilers/openapi.md#union-beside-structure), whether the union is
// written beside the allOf or on an entry of it, its provenance saying which.
// Each draws one info at the branch.
func TestOneOf_AKeptBranchIsKeyedByItsKeywordAndIndex(t *testing.T) {
	t.Parallel()
	doc, diags := schema.LowerSpec(t, openapitest.ComponentSpec(`    Obj: {type: object, properties: {o: {type: string}}}
    B: {type: object, properties: {b: {type: integer}}}
    Beside:
      allOf: [{$ref: '#/components/schemas/Obj'}]
      oneOf: [{$ref: '#/components/schemas/B'}, {anyOf: [{type: object}, {type: string}]}]
    Entry:
      allOf: [{$ref: '#/components/schemas/Obj'}, {oneOf: [{$ref: '#/components/schemas/B'}, {anyOf: [{type: object}, {type: string}]}]}]
`))
	openapitest.RequireNoErrorDiags(t, diags)

	for name, at := range map[string]string{"Beside": "/oneOf/1", "Entry": "/allOf/1/oneOf/1"} {
		const key = "openapi:oneOf/1"
		u, ok := typeByName(doc, name).(*ir.Union)
		require.True(t, ok, "%s distributes", name)
		require.Len(t, u.Variants, 2, name)
		v, ok := doc.Types[u.Variants[1].Type.Target].(*ir.Model)
		require.True(t, ok, name)
		require.NotNil(t, v.Base, name)
		assert.Equal(t, componentID("Obj"), v.Base.Target, "%s: the composition's base, and no union", name)
		assert.Empty(t, v.Mixins, name)
		assert.Equal(t, []string{key}, slices.Sorted(maps.Keys(v.Unmodeled)), name)
		assert.JSONEq(t, `{"anyOf":[{"type":"object"},{"type":"string"}]}`, string(v.Unmodeled[key].Value), name)
		at = "/components/schemas/" + name + at
		assert.Equal(t, at, string(v.Unmodeled[key].Provenance.Pointer), name)
		assert.Len(t, diagsAtPointer(diags, diag.DegradedConstruct, at), 1, "one info at %s", at)
	}
}
