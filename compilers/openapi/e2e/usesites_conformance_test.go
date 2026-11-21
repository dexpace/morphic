// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"encoding/json/v2"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// assertCatchAllEncoding is the corpus row for a form body's catch-all: a
// model's value beside a declared part, a body that is a map, and a union body
// whose variants each hold a catch-all, each keyed by the type holding it.
func assertCatchAllEncoding(t *testing.T, doc *ir.Document, diags []ir.Diagnostic) {
	t.Helper()
	assert.Empty(t, diags)
	const schema = "/requestBody/content/multipart~1form-data/schema"
	octet := `"contentTypes":["application/octet-stream"]`
	for source, want := range map[string]string{
		"putSnippet": `{"type/anonymous/paths/~1snippet/put` + schema + `":{"value":{` + octet +
			`,"repeatsPerArrayItem":true,"hasFilename":true,"allowReserved":false}}}`,
		"uploadAssets": `{"type/anonymous/paths/~1assets/post` + schema + `":{"value":{` + octet +
			`,"repeatsPerArrayItem":false,"hasFilename":true,"allowReserved":false}}}`,
		"mixed": `{"type/openapi/components/schemas/Files":{"patterns":{"^f_":{` + octet +
			`,"repeatsPerArrayItem":false,"hasFilename":true,"allowReserved":false}}},` +
			`"type/openapi/components/schemas/Notes":{"value":{"contentTypes":["text/plain"],` +
			`"repeatsPerArrayItem":false,"hasFilename":false,"allowReserved":false}}}`,
	} {
		got, err := json.Marshal(valueAtPath(operationTree(t, doc, source), "request", "contents", "0", "catchAllEncoding"),
			json.Deterministic(true))
		require.NoError(t, err)
		assert.JSONEq(t, want, string(got), source)
	}
}

// TestCatchAllEncoding_IsKeptAndVerifies compiles catch-all-encoding through a
// verified run, passes included: the field the lowering fills is one the
// document verifier and pass.Validate accept, and the run draws no finding.
func TestCatchAllEncoding_IsKeptAndVerifies(t *testing.T) {
	t.Parallel()
	doc, diags := compileNamedVerified(t, "catch-all-encoding.yaml", corpusFile(t, "catch-all-encoding"))
	assertCatchAllEncoding(t, doc, diags)
}
