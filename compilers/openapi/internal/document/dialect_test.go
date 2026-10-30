// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package document_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// TestMeta_JSONSchemaDialectIsKept pins the document's jsonSchemaDialect, which
// nothing read: it is kept verbatim on the document with an info, as a schema's
// own `$schema` is, written directly or merged in through `<<`, and in a 3.0
// document, which it postdates, load also reports it. Declaring none keeps
// nothing.
func TestMeta_JSONSchemaDialectIsKept(t *testing.T) {
	t.Parallel()
	const dialect = "https://json-schema.org/draft/2020-12/schema"
	for _, tc := range []struct {
		name, version, line string
	}{
		{"3.1", "3.1.0", "jsonSchemaDialect: " + dialect},
		{"3.1 merged in", "3.1.0", "<<: {jsonSchemaDialect: '" + dialect + "'}"},
		{"3.0", "3.0.3", "jsonSchemaDialect: " + dialect},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, diags := parseFull(t, "openapi: "+tc.version+"\n"+tc.line+"\ninfo: {title: T, version: \"1\"}\npaths: {}\n")
			kept, ok := doc.Unmodeled["openapi:jsonSchemaDialect"]
			require.True(t, ok, "the dialect is kept on the document")
			assert.Equal(t, ir.ReasonOutOfScope, kept.Reason)
			assert.JSONEq(t, `"`+dialect+`"`, string(kept.Value))
			openapitest.AssertInfoDiagAt(t, diags, "/jsonSchemaDialect")
			assert.Equal(t, tc.version == "3.0.3",
				openapitest.HasDiagCodeAt(diags, diag.LaterVersionConstruct, "/jsonSchemaDialect"))
		})
	}
	doc, _ := parseFull(t, "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths: {}\n")
	assert.NotContains(t, doc.Unmodeled, "openapi:jsonSchemaDialect")
}
