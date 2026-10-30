// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package operation_test

import (
	"encoding/json/jsontext"
	"maps"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// TestEncoding_AllowReservedAndExtensionsReachTheIR pins both halves of GitHub
// #291. Neither allowReserved nor an encoding's x-* reached an IR field, an
// Unmodeled entry or a diagnostic, so two documents differing only in them
// compiled to the same IR. allowReserved lowers onto the part's PartEncoding,
// as a parameter's does onto its binding; PartEncoding carries no Unmodeled
// map, so the x-* lands on the owning Content keyed by the part it governs.
func TestEncoding_AllowReservedAndExtensionsReachTheIR(t *testing.T) {
	t.Parallel()
	_, svc, diags := compileService(t, openapitest.PathsSpecVer("3.1.0", encodingSpec))
	openapitest.RequireNoErrorDiags(t, diags)
	op := openapitest.FirstOp(t, svc)
	require.NotNil(t, op.Request)
	require.Len(t, op.Request.Contents, 1)
	content := op.Request.Contents[0]
	kept := content.Unmodeled

	require.Len(t, content.Encoding, 1)
	for _, pe := range content.Encoding {
		assert.True(t, pe.AllowReserved, "allowReserved lowers onto the part")
	}
	assert.NotContains(t, kept, "openapi:encoding/q/allowReserved", "and is kept verbatim nowhere")
	assert.False(t, openapitest.HasDiag(diags, diag.DegradedConstruct), "nothing is degraded: %+v", diags)

	at := "/paths/~1form/post/requestBody/content/application~1x-www-form-urlencoded/encoding/q"
	ext, ok := kept["openapi:encoding/q/x-vendor"]
	require.True(t, ok, "the encoding's own x-* is kept; got %v", kept)
	assert.Equal(t, ir.ReasonVendorExtension, ext.Reason)
	assert.Equal(t, jsontext.Pointer(at+"/x-vendor"), ext.Provenance.Pointer)
}

// TestEncoding_AbsentAllowReservedRecordsNothing is the control: an entry that
// omits allowReserved records neither an entry nor a diagnostic, and its part
// reads the OpenAPI default, false.
func TestEncoding_AbsentAllowReservedRecordsNothing(t *testing.T) {
	t.Parallel()
	_, svc, diags := compileService(t, openapitest.PathsSpecVer("3.1.0", `  /form:
    post:
      operationId: postForm
      requestBody:
        required: true
        content:
          application/x-www-form-urlencoded:
            schema: {type: object, properties: {q: {type: string}}}
            encoding:
              q: {style: form}
      responses: {"200": {description: ok}}
`))
	openapitest.RequireNoErrorDiags(t, diags)
	op := openapitest.FirstOp(t, svc)
	require.NotNil(t, op.Request)
	require.Len(t, op.Request.Contents, 1)
	assert.Empty(t, op.Request.Contents[0].Unmodeled, "nothing was declared, so nothing is kept")
	for _, pe := range op.Request.Contents[0].Encoding {
		assert.False(t, pe.AllowReserved)
	}
	assert.Empty(t, diags)
}

// TestEncoding_OnlyUnhomedFieldsOutliveTheEmptyPartEncoding pins the ordering
// inside partEncodings. An entry declaring nothing ir.PartEncoding has a field
// for lowers to an empty one, which is dropped from Content.Encoding — so the
// entry has to be read before that check rather than after it, or what it did
// declare goes with it. An entry declaring allowReserved alone is the other
// half: that is a field, so its part is kept.
//
// Its own case because every other encoding fixture declares a style or a
// contentType, which leaves the PartEncoding non-empty and routes around the
// check entirely: moving the read below it left the whole suite green.
func TestEncoding_OnlyUnhomedFieldsOutliveTheEmptyPartEncoding(t *testing.T) {
	t.Parallel()
	_, svc, diags := compileService(t, openapitest.PathsSpecVer("3.1.0", `  /form:
    post:
      operationId: postForm
      requestBody:
        required: true
        content:
          application/x-www-form-urlencoded:
            schema: {type: object, properties: {q: {type: string}, r: {type: string}}}
            encoding:
              q: {x-vendor: vvv}
              r: {allowReserved: true}
      responses: {"200": {description: ok}}
`))
	openapitest.RequireNoErrorDiags(t, diags)
	op := openapitest.FirstOp(t, svc)
	require.NotNil(t, op.Request)
	require.Len(t, op.Request.Contents, 1)
	content := op.Request.Contents[0]
	require.Len(t, content.Encoding, 1,
		"the part declaring only x-* lowers to an empty PartEncoding and is left out; got %v", content.Encoding)
	for id, pe := range content.Encoding {
		assert.Contains(t, string(id), "/properties/r", "the part kept is the one declaring allowReserved")
		assert.True(t, pe.AllowReserved, "and it keeps what it declared")
	}
	assert.Contains(t, content.Unmodeled, "openapi:encoding/q/x-vendor", "the empty part's own x-* outlives it")
}

// TestEncoding_PartNameWithSlashKeepsItsOwnKey pins that a part's name is one
// scope segment however it is spelled. The scope is "encoding/<part>" and a
// property name can contain "/", which must not read as a separator: parts "q"
// and "q/x-a" would otherwise share one key, and the surviving entry would
// follow declaration order.
//
// The two entries must be distinct and each must hold its own part's value;
// asserting only that two keys exist would pass on a lowering that swapped
// them.
func TestEncoding_PartNameWithSlashKeepsItsOwnKey(t *testing.T) {
	t.Parallel()
	_, svc, diags := compileService(t, openapitest.PathsSpecVer("3.1.0", `  /form:
    post:
      operationId: postForm
      requestBody:
        required: true
        content:
          application/x-www-form-urlencoded:
            schema:
              type: object
              properties:
                q: {type: string}
                "q/x-a": {type: string}
            encoding:
              q: {style: form, x-a/x-b: FROM_Q}
              "q/x-a": {style: form, x-b: FROM_Q_SLASH_XA}
      responses: {"200": {description: ok}}
`))
	openapitest.RequireNoErrorDiags(t, diags)
	kept := openapitest.FirstOp(t, svc).Request.Contents[0].Unmodeled

	plain, ok := kept["openapi:encoding/q/x-a/x-b"]
	require.True(t, ok, `the part named "q" keeps its own x-a/x-b; got %v`, kept)
	assert.JSONEq(t, `"FROM_Q"`, string(plain.Value))

	slashed, ok := kept["openapi:encoding/q~1x-a/x-b"]
	require.True(t, ok, `the part named "q/x-a" keeps its own x-b under an escaped scope; got %v`, kept)
	assert.JSONEq(t, `"FROM_Q_SLASH_XA"`, string(slashed.Value))
}

// TestEncoding_ItemEncodingKeepsItsOwnUnderItsOwnKey is the other position the
// same reader serves. A content can hold both a per-part encoding map and a
// sequential itemEncoding, and both reach one Unmodeled map, so the key has to
// tell them apart.
func TestEncoding_ItemEncodingKeepsItsOwnUnderItsOwnKey(t *testing.T) {
	t.Parallel()
	_, svc, diags := compileService(t, openapitest.PathsSpecVer("3.2.0", `  /stream:
    get:
      operationId: streamEvents
      responses:
        "200":
          description: ok
          content:
            multipart/mixed:
              itemSchema: {type: object, properties: {n: {type: string}}}
              itemEncoding: {contentType: application/json, allowReserved: true, x-vendor: vvv}
`))
	openapitest.RequireNoErrorDiags(t, diags)
	op := openapitest.FirstOp(t, svc)
	require.NotEmpty(t, op.Responses)
	require.NotNil(t, op.Responses[0].Payload)
	require.Len(t, op.Responses[0].Payload.Contents, 1)
	content := op.Responses[0].Payload.Contents[0]

	require.NotNil(t, content.ItemEncoding)
	assert.True(t, content.ItemEncoding.AllowReserved, "the item encoding's allowReserved lowers onto it")
	assert.Contains(t, content.Unmodeled, "openapi:itemEncoding/x-vendor")
}

// TestEncoding_NestedEncodingsAreKeptAtEveryVersion pins GitHub #615's nested
// Encoding fields: 3.2 gives an Encoding Object an encoding map and the
// positional prefix/item encodings, ir.PartEncoding has no field for any of
// them, and the bundled model does not name them either — so they used to draw
// unknown-object-key warnings and reach no entry. Each is kept verbatim under
// the part's own scope with one info, and below 3.2 load adds that the field is
// a later version's (GitHub #864).
func TestEncoding_NestedEncodingsAreKeptAtEveryVersion(t *testing.T) {
	t.Parallel()
	const body = `  /upload:
    post:
      operationId: upload
      requestBody:
        content:
          multipart/form-data:
            schema:
              type: object
              properties:
                note: {type: string}
            encoding:
              note:
                contentType: text/plain
                prefixEncoding: [{contentType: text/plain}]
      responses: {"200": {description: ok}}
`
	const at = "/paths/~1upload/post/requestBody/content/multipart~1form-data/encoding/note/prefixEncoding"
	for _, version := range []string{"3.2.0", "3.1.0"} {
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			_, svc, diags := compileService(t, "openapi: "+version+"\ninfo: {title: T, version: \"1\"}\npaths:\n"+body)
			openapitest.RequireNoErrorDiags(t, diags)
			content := openapitest.FirstOp(t, svc).Request.Contents[0]
			kept, ok := content.Unmodeled["openapi:encoding/note/prefixEncoding"]
			require.True(t, ok, "the nested prefixEncoding is kept under the part's own scope; got %v",
				slices.Sorted(maps.Keys(content.Unmodeled)))
			assert.Equal(t, ir.ReasonNoIRHome, kept.Reason)
			openapitest.AssertInfoDiagAt(t, diags, at)
			assert.False(t, openapitest.HasDiag(diags, diag.UnknownObjectKey),
				"the reader took the keys, so the census must leave them alone")
			assert.Equal(t, version != "3.2.0", openapitest.HasDiagCodeAt(diags, diag.LaterVersionConstruct, at),
				"only a document older than 3.2 is told the field is a later version's")
		})
	}
}

// encodingSpec declares one form-urlencoded part, q, whose Encoding Object
// writes the two things ir.PartEncoding has no field for, allowReserved and an
// x-* extension, beside a style it does.
const encodingSpec = `  /form:
    post:
      operationId: postForm
      requestBody:
        required: true
        content:
          application/x-www-form-urlencoded:
            schema: {type: object, properties: {q: {type: string}}}
            encoding:
              q: {style: form, allowReserved: true, x-vendor: vvv}
      responses: {"200": {description: ok}}
`
