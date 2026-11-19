// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

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
)

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
        - {type: [object, string], minLength: 3}
        - {$ref: '#/components/schemas/TL', minProperties: 2}
`))
	openapitest.RequireNoErrorDiags(t, diags)

	union, ok := typeByName(doc, "S").(*ir.Union)
	require.True(t, ok)
	require.Len(t, union.Variants, 4)
	for i := 1; i < len(union.Variants); i++ {
		model, ok := doc.Types[union.Variants[i].Type.Target].(*ir.Model)
		require.True(t, ok, "variant %d", i)
		assert.Contains(t, model.Unmodeled, fmt.Sprintf("openapi:oneOf/%d", i), "variant %d keeps its branch", i)
		at := fmt.Sprintf("/components/schemas/S/oneOf/%d", i)
		assert.NotContains(t, doc.Types, ir.TypeID("type/anonymous"+at), "no node nothing names holds branch %d again", i)
		assert.Len(t, diagsAtPointer(diags, diag.DegradedConstruct, at), 1, "one info at %s", at)
	}
}
