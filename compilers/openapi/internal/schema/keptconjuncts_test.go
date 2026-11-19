// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/schema"
	"github.com/dexpace/morphic/ir"
)

// TestValueSet_APassedOverAllOfWritingAnInlineEntryIsKeptWhole pins the allOf
// the dispatch among competing keywords passes over where it writes an inline
// entry: kept whole under openapi:allOf, the $ref beside it under
// openapi:$ref, where an allOf of $ref entries alone keeps each by its text.
func TestValueSet_APassedOverAllOfWritingAnInlineEntryIsKeptWhole(t *testing.T) {
	t.Parallel()
	doc, diags := schema.LowerSpec(t, openapitest.ComponentSpecVer("3.1.0", `    A: {type: object, properties: {a: {type: string}}}
    S: {enum: [], $ref: '#/components/schemas/A', allOf: [{required: [a]}]}
`))
	en, ok := typeByName(doc, "S").(*ir.Enum)
	require.True(t, ok, "got %T", typeByName(doc, "S"))
	assert.ElementsMatch(t, []string{"openapi:$ref", "openapi:allOf"}, unmodeledKeys(en.Unmodeled))
	assert.JSONEq(t, `"#/components/schemas/A"`, string(en.Unmodeled["openapi:$ref"].Value))
	assert.JSONEq(t, `[{"required":["a"]}]`, string(en.Unmodeled["openapi:allOf"].Value))
	assert.Contains(t, openapitest.DiagMessageAt(t, diags, diag.DegradedConstruct, ir.SeverityInfo, "/components/schemas/S"),
		"declares $ref and allOf beside its enum")
}
