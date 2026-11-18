// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
)

// TestStand_AKeywordThePositionKeepsOtherwiseIsKeptUnderThePrincipalsEntry pins
// a keyword an inline principal and its position both keep: written otherwise,
// the plain key holds the position's and the entry's key the principal's alone;
// written alike, it is kept once; where the position keeps the principal whole,
// the whole entry holds it; and where the principal's value cannot be kept, it
// is reported lost and announced nowhere.
func TestStand_AKeywordThePositionKeepsOtherwiseIsKeptUnderThePrincipalsEntry(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.ComponentSpec(
		"    Apart: {allOf: [{type: string, minItems: 1}], minItems: 2}\n"+
			"    Alike: {allOf: [{type: string, minItems: 2}], minItems: 2}\n"+
			"    Whole: {allOf: [{type: string, minItems: 1, x-k: 1}], minItems: 2, x-k: 2}\n"+
			"    Lost: {allOf: [{type: string, minimum: "+unpreservableValue+"}], minimum: 2}\n"+
			"    Text: {allOf: [{type: string, format: double, minimum: 1, x-k: 1}], minimum: 2, x-k: 2}\n"+
			"    List: {allOf: [{type: [string, boolean], format: int64, minimum: 1, x-k: 1}], x-k: 2}\n"))
	held := func(name string) map[string]string {
		td := typeByName(doc, name)
		require.NotNil(t, td, name)
		out := map[string]string{}
		for key, e := range td.Common().Unmodeled {
			out[key] = string(e.Value)
		}
		return out
	}
	assert.Equal(t, map[string]string{"openapi:minItems": "2", "openapi:allOf/0": `{"minItems":1}`}, held("Apart"))
	assert.Equal(t, map[string]string{"openapi:minItems": "2"}, held("Alike"))
	whole := held("Whole")
	assert.JSONEq(t, `{"type":"string","minItems":1,"x-k":1}`, whole["openapi:allOf/0"], "the whole entry holds the principal's")
	assert.Equal(t, "2", whole["openapi:minItems"])
	assert.Equal(t, map[string]string{"openapi:minimum": "2"}, held("Lost"))
	assert.True(t, openapitest.HasDiag(diags, diag.UnpreservableConstruct), "the value that cannot be kept is reported")
	text := held("Text")
	assert.JSONEq(t, `{"type":"string","format":"double","minimum":1,"x-k":1}`, text["openapi:allOf/0"],
		"the whole entry holds a number as text's bound")
	assert.Equal(t, "2", text["openapi:minimum"])
	list := held("List")
	assert.JSONEq(t, `{"type":["string","boolean"],"format":"int64","minimum":1,"x-k":1}`, list["openapi:allOf/0"])
	assert.NotContains(t, list, "openapi:minimum", "the union keeps none beside the whole entry")
	variant := doc.Types["t/anon/components/schemas/List/type/0"]
	require.NotNil(t, variant)
	assert.NotContains(t, variant.Common().Unmodeled, "openapi:minimum", "nor does the text variant")
}
