// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// runPromoterSpec writes a key each step reading the whole document reads
// under runPromoterPolicy, and none any other policy maps: a pagination, a
// choice among parameters, a stream's terminator, a replacement both named and
// written in prose, the redaction of a credential, and an unread key under an
// owned prefix.
const runPromoterSpec = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /x:
    post:
      operationId: x
      x-acme-page: {link: $response.body#/next}
      x-acme-choices:
        side: {optional: true, variants: {left: [left], right: [right]}}
      x-acme-unread: 1
      parameters:
        - {name: left, in: query, schema: {type: string}}
        - {name: right, in: query, schema: {type: string}}
        - {name: api_key, in: query, x-acme-secret: false, schema: {type: string}}
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema: {type: object, properties: {next: {type: string}}}
            text/event-stream:
              x-acme-end: "[DONE]"
              schema: {type: string}
  /old:
    get:
      operationId: old
      deprecated: true
      x-acme-why: Use GET /other.
      x-acme-instead: new
      responses: {"200": {description: ok}}
  /new:
    get: {operationId: new, responses: {"200": {description: ok}}}
  /other:
    get: {operationId: other, responses: {"200": {description: ok}}}
`

// runPromoterPolicy maps every key runPromoterSpec writes, under the prefix it
// owns, and only those.
func runPromoterPolicy() openapi.ExtensionPromotions {
	return openapi.ExtensionPromotions{
		Prefixes: []string{"x-acme-"},
		Targets: map[openapi.ExtensionTarget][]openapi.ExtensionSource{
			openapi.TargetPaginationNextLink:    {{Key: "x-acme-page", Member: "link"}},
			openapi.TargetParamChoices:          {{Key: "x-acme-choices"}},
			openapi.TargetStreamTerminator:      {{Key: "x-acme-end"}},
			openapi.TargetDeprecationMessage:    {{Key: "x-acme-why"}},
			openapi.TargetDeprecationReplacedBy: {{Key: "x-acme-instead"}},
			openapi.TargetRedaction:             {{Key: "x-acme-secret"}},
		},
	}
}

// runPromoterOps compiles runPromoterSpec under promotions and returns its
// operations by ID, its api_key parameter, and its unread-extension reports.
func runPromoterOps(t *testing.T, promotions openapi.ExtensionPromotions) (map[ir.OperationID]ir.Operation, ir.Parameter, int) {
	t.Helper()
	doc, diags, err := openapi.New().Compile(t.Context(), []compilers.Source{openapitest.SourceOf(runPromoterSpec)},
		compilers.Options{FormatOptions: openapi.Options{Promotions: promotions}})
	require.NoError(t, err)
	require.NotNil(t, doc)
	ops := map[ir.OperationID]ir.Operation{}
	for _, op := range allOperations(doc) {
		ops[op.ID] = op
	}
	var key ir.Parameter
	for _, p := range ops["operation/openapi/paths/~1x/post"].Params {
		if p.Name.Source == "api_key" {
			key = p
		}
	}
	require.NotEmpty(t, key.ID, "the spec's api_key parameter")
	unread := 0
	for _, d := range diags {
		if d.Code == "openapi/unread-extension" {
			unread++
		}
	}
	return ops, key, unread
}

// TestRun_EveryWholeDocumentReaderReadsTheRunsPromotionPolicy pins the policy
// the compiler's run hands each step reading the whole document to the one the
// caller set: under it each step reads its key, and with it disabled none
// reads any, though the credential's name then asks for its redaction.
func TestRun_EveryWholeDocumentReaderReadsTheRunsPromotionPolicy(t *testing.T) {
	t.Parallel()
	ops, key, unread := runPromoterOps(t, runPromoterPolicy())
	x, old := ops["operation/openapi/paths/~1x/post"], ops["operation/openapi/paths/~1old/get"]
	require.NotNil(t, x.Pagination, "pagination")
	assert.Equal(t, ir.PageStrategyNextLink, x.Pagination.Strategy)
	assert.Len(t, x.ParamChoices, 1, "parameter choices")
	require.NotNil(t, x.ResponseStream, "stream")
	require.NotNil(t, x.ResponseStream.Terminator, "stream terminator")
	assert.Equal(t, "[DONE]", *x.ResponseStream.Terminator)
	require.NotNil(t, old.Deprecation)
	assert.Equal(t, []ir.EntityRef{{Operation: "operation/openapi/paths/~1new/get"}}, old.Deprecation.ReplacedBy,
		"the key names the replacement, and the message's is not read beside it")
	assert.False(t, key.Secret, "the key says the credential is not redacted")
	assert.NotContains(t, string(key.Provenance.Inferred), "secret-inference")
	assert.Equal(t, 1, unread, "x-acme-unread is under the owned prefix and read by nothing")

	ops, key, unread = runPromoterOps(t, openapi.ExtensionPromotions{Disabled: true})
	x, old = ops["operation/openapi/paths/~1x/post"], ops["operation/openapi/paths/~1old/get"]
	assert.Nil(t, x.Pagination)
	assert.Empty(t, x.ParamChoices)
	assert.True(t, x.ResponseStream == nil || x.ResponseStream.Terminator == nil, "no terminator")
	assert.True(t, old.Deprecation == nil || old.Deprecation.ReplacedBy == nil, "no replacement")
	assert.True(t, key.Secret, "no key is read, so the credential's name asks for its redaction")
	assert.Contains(t, string(key.Provenance.Inferred), "secret-inference")
	assert.Zero(t, unread)
}
