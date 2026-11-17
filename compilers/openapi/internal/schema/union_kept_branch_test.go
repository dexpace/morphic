// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema_test

import (
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/schema"
	"github.com/dexpace/morphic/ir"
)

// TestOneOf_AKeptBranchIsKeyedByItsPathFromTheSchema pins the key a variant
// keeps a branch it does not compose under: the branch's path from the schema
// (docs/compilers/openapi.md#union-beside-structure), so a union written on an
// allOf entry keeps it under that entry's path, and one beside the allOf under
// its keyword and index alone. Each draws one info at the branch.
func TestOneOf_AKeptBranchIsKeyedByItsPathFromTheSchema(t *testing.T) {
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

	for name, key := range map[string]string{"Beside": "openapi:oneOf/1", "Entry": "openapi:allOf/1/oneOf/1"} {
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
		at := "/components/schemas/" + name + "/" + strings.TrimPrefix(key, "openapi:")
		assert.Len(t, diagsAtPointer(diags, diag.DegradedConstruct, at), 1, "one info at %s", at)
	}
}
