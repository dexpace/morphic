// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package operation_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

func TestContent_PositionalPrefixEncodingIsPreserved(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, positionalEncodingSpec)
	c := openapitest.FindOp(t, doc, "mixed").Responses[0].Payload.Contents[0]

	assert.Nil(t, c.ItemEncoding, "positional prefixes rule out an every-item encoding")
	prefix, ok := c.Unmodeled["openapi:prefixEncoding"]
	require.True(t, ok, "prefixEncoding kept verbatim; got %v", c.Unmodeled)
	assert.Equal(t, ir.ReasonNoIRHome, prefix.Reason)
	assert.JSONEq(t, `[{"contentType": "application/json"}, {"contentType": "application/xml"}]`,
		string(prefix.Value))
	item, ok := c.Unmodeled["openapi:itemEncoding"]
	require.True(t, ok, "the tail encoding is kept beside the prefixes it follows")
	assert.JSONEq(t, `{"contentType": "text/plain"}`, string(item.Value))
	openapitest.AssertHasCode(t, diags, diag.DegradedConstruct, ir.SeverityInfo)
}

func TestFillSequential_PrefixEncodingWithoutItemEncoding(t *testing.T) {
	t.Parallel()
	spec := strings.ReplaceAll(positionalEncodingSpec, "              itemEncoding: {contentType: text/plain}\n", "")
	doc, diags := parseFull(t, spec)
	c := openapitest.FindOp(t, doc, "mixed").Responses[0].Payload.Contents[0]

	assert.Nil(t, c.ItemEncoding)
	_, ok := c.Unmodeled["openapi:prefixEncoding"]
	assert.True(t, ok, "prefixEncoding alone is still reported rather than dropped")
	_, ok = c.Unmodeled["openapi:itemEncoding"]
	assert.False(t, ok, "no itemEncoding was declared, so none is recorded")
	openapitest.AssertHasCode(t, diags, diag.DegradedConstruct, ir.SeverityInfo)
}

// TestContent_AMergedPositionalEncodingIsPreserved pins that prefixEncoding
// and itemEncoding merged in through `<<` are kept as written ones are (GitHub
// #384). The parser reads them, so this lowering runs, and a raw read that
// missed them would report a declared prefixEncoding it could not find.
func TestContent_AMergedPositionalEncodingIsPreserved(t *testing.T) {
	t.Parallel()
	written := "              prefixEncoding:\n                - contentType: application/json\n" +
		"                - contentType: application/xml\n              itemEncoding: {contentType: text/plain}\n"
	merged := "              <<: {prefixEncoding: [{contentType: application/json}, {contentType: application/xml}], " +
		"itemEncoding: {contentType: text/plain}}\n"
	require.Contains(t, positionalEncodingSpec, written, "sanity: the spec writes both in place")
	doc, diags := parseFull(t, strings.Replace(positionalEncodingSpec, written, merged, 1))
	c := openapitest.FindOp(t, doc, "mixed").Responses[0].Payload.Contents[0]

	assert.False(t, openapitest.HasDiag(diags, diag.UnpreservableConstruct), "%+v", diags)
	assert.JSONEq(t, `[{"contentType": "application/json"}, {"contentType": "application/xml"}]`,
		string(c.Unmodeled["openapi:prefixEncoding"].Value))
	assert.JSONEq(t, `{"contentType": "text/plain"}`, string(c.Unmodeled["openapi:itemEncoding"].Value))
}

// positionalEncodingSpec declares the 3.2 shape ItemEncoding cannot state:
// prefixEncoding fixes the encoding of the leading items, so the itemEncoding
// beside it governs only the tail after them, not every item.
const positionalEncodingSpec = `openapi: 3.2.0
info: {title: T, version: "1"}
paths:
  /mixed:
    get:
      operationId: mixed
      responses:
        "200":
          description: ok
          content:
            multipart/mixed:
              itemSchema: {type: object, properties: {a: {type: string}}}
              prefixEncoding:
                - contentType: application/json
                - contentType: application/xml
              itemEncoding: {contentType: text/plain}
`
