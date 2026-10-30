// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package operation_test

import (
	"encoding/json/jsontext"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/engine/reader"
	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irverify"
)

// pathItemRefChainSpec chains /a to /b to /c, with fields beside both $refs.
const pathItemRefChainSpec = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a:
    $ref: '#/paths/~1b'
    summary: from a
    get:
      operationId: getFromA
      responses: {"200": {description: OK}}
  /b:
    $ref: '#/paths/~1c'
    summary: from b
    servers: [{url: https://b.example}]
    get:
      operationId: getFromB
      responses: {"200": {description: OK}}
    put:
      operationId: putFromB
      responses: {"200": {description: OK}}
  /c:
    get:
      operationId: getFromC
      responses: {"200": {description: OK}}
    post:
      operationId: postFromC
      responses: {"200": {description: OK}}
`

// TestPathItemRefSiblings_AChainCarriesTheFieldsBesideEachRef pins a chain: the
// path item a $ref names is that item with the fields beside its own $ref, so
// /a mounts /b's put and /c's post, takes /b's servers, and keeps its own get
// and summary over both deeper ones, each loser named once.
func TestPathItemRefSiblings_AChainCarriesTheFieldsBesideEachRef(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, pathItemRefChainSpec)
	openapitest.RequireNoErrorDiags(t, diags)
	assert.Empty(t, irverify.Verify(doc))

	for method, decl := range map[string]jsontext.Pointer{
		"GET": "/paths/~1a/get", "PUT": "/paths/~1b/put", "POST": "/paths/~1c/post",
	} {
		op := opByPath(t, doc, method, "/a")
		assert.Equal(t, decl, op.Provenance.Pointer, "%s at /a is declared where the chain writes it", method)
		summary, ok := op.Unmodeled["openapi:pathItemSummary"]
		require.True(t, ok)
		assert.JSONEq(t, `"from a"`, string(summary.Value))
		assert.Equal(t, []string{"https://b.example"}, serversNamed(t, doc, op.Servers),
			"%s at /a is sent to the servers beside /b's $ref", method)
	}
	var collided []string
	for _, d := range diags {
		if d.Code == diag.PathItemRefCollision {
			_, other, _ := strings.Cut(d.Message, "and again at ")
			other, _, _ = strings.Cut(other, " in the item")
			collided = append(collided, string(d.Provenance.Pointer)+" "+other)
		}
	}
	slices.Sort(collided)
	assert.Equal(t, []string{
		"/paths/~1a/get /paths/~1b/get",
		"/paths/~1a/get /paths/~1c/get",
		"/paths/~1a/summary /paths/~1b/summary",
		"/paths/~1b/get /paths/~1c/get",
	}, collided, "each declaration a nearer one takes over is named once, at the one kept")
}

// TestPathItemRefSiblings_FieldsAnotherDocumentWritesAreReported pins the chain
// this compiler cannot read all of: the fields beside a $ref in another
// document are not modelled, and saying so is what keeps them from being
// dropped in silence.
func TestPathItemRefSiblings_FieldsAnotherDocumentWritesAreReported(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "other.yaml"), []byte(`openapi: 3.1.0
info: {title: O, version: "1"}
paths:
  /x:
    $ref: '#/components/pathItems/P'
    put: {responses: {"200": {description: OK}}}
components:
  pathItems:
    P:
      get: {responses: {"200": {description: OK}}}
`), 0o600))
	doc, diags, err := openapi.New().Compile(t.Context(),
		[]compilers.Source{{Path: filepath.Join(dir, "main.yaml"), Data: []byte(`openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a: {$ref: './other.yaml#/paths/~1x'}
`)}}, compilers.Options{Reader: reader.Disk{}, FormatOptions: openapi.Options{AllowExternalRefs: true}})
	require.NoError(t, err)
	require.NotNil(t, doc)

	opByPath(t, doc, "GET", "/a")
	message := openapitest.DiagMessageAt(t, diags, diag.DegradedConstruct, ir.SeverityWarning, "/paths/~1a")
	assert.Contains(t, message, "./other.yaml#/paths/~1x", "the reference reaching the unread fields is named")
	assert.Contains(t, message, "in another document")
	assert.Contains(t, message, "not lowered")
}

// TestPathItemRefSiblings_FieldsTheWalkDoesNotReachAreReported pins the other
// chain this compiler cannot read all of: a hop in this document at a position
// no object its walk holds, here under an x-* key, is not modelled either, and
// the report says where it is rather than calling it another document's.
func TestPathItemRefSiblings_FieldsTheWalkDoesNotReachAreReported(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, `openapi: 3.1.0
info: {title: T, version: "1"}
x-shared:
  item:
    $ref: '#/paths/~1b'
    put: {operationId: putShared, responses: {"200": {description: OK}}}
paths:
  /a: {$ref: '#/x-shared/item'}
  /b:
    get: {operationId: getB, responses: {"200": {description: OK}}}
`)
	openapitest.RequireNoErrorDiags(t, diags)

	assert.Equal(t, "getB", opByPath(t, doc, "GET", "/a").Name.Source, "the item the chain ends at still mounts")
	message := openapitest.DiagMessageAt(t, diags, diag.DegradedConstruct, ir.SeverityWarning, "/paths/~1a")
	assert.Contains(t, message, "#/x-shared/item", "the reference reaching the unread fields is named")
	assert.Contains(t, message, "does not reach")
	assert.NotContains(t, message, "another document", "the hop is this document's")
}

// pathItemRefCallbackSiblingSpec writes an expression beside a Callback
// Object's $ref, which OpenAPI says to ignore: a Reference Object's other
// fields are not the callback's.
const pathItemRefCallbackSiblingSpec = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /one:
    post:
      operationId: one
      callbacks:
        cb:
          $ref: '%s#/components/callbacks/CB'
          '{$request.body#/url}':
            $ref: '#/components/pathItems/T'
            patch:
              operationId: ignoredSibling
              responses: {"200": {description: OK}}
      responses: {"200": {description: OK}}
components:
  pathItems:
    T:
      post: {responses: {"200": {description: OK}}}
  callbacks:
    CB:
      '{$request.body#/url}':
        post:
          operationId: cbPost
          responses: {"200": {description: OK}}
`

// TestPathItemRefSiblings_AnExpressionBesideACallbackRefIsIgnored pins where
// the fields beside a $ref are read from: the source, at the $ref that writes
// them, never at a position another document's content was mounted at, so an
// expression beside a Callback Object's $ref is ignored however the callback
// resolves.
func TestPathItemRefSiblings_AnExpressionBesideACallbackRefIsIgnored(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	other := strings.Replace(pathItemRefCallbackSiblingSpec, "%s", "", 1)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "other.yaml"), []byte(other), 0o600))
	for _, at := range []string{"", "./other.yaml"} {
		doc, _, err := openapi.New().Compile(t.Context(),
			[]compilers.Source{{Path: filepath.Join(dir, "main.yaml"), Data: []byte(strings.Replace(pathItemRefCallbackSiblingSpec, "%s", at, 1))}},
			compilers.Options{Reader: reader.Disk{}, FormatOptions: openapi.Options{AllowExternalRefs: true}})
		require.NoError(t, err)
		require.NotNil(t, doc)
		openapitest.FindOp(t, doc, "cbPost")
		for _, g := range doc.Services[0].Groups {
			for _, op := range g.Operations {
				assert.NotEqual(t, "ignoredSibling", op.Name.Source, "callback %q", at)
			}
		}
	}
}

// TestPathItemRefSiblings_MalformedFieldsAreReported pins that the fields
// beside a $ref are validated as an inline item's are: a value of the wrong
// type is an error where it is written, not accepted in silence.
func TestPathItemRefSiblings_MalformedFieldsAreReported(t *testing.T) {
	t.Parallel()
	_, diags := parseFull(t, `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a:
    $ref: '#/paths/~1b'
    put:
      deprecated: notabool
      parameters: [{name: q, in: query, required: maybe, schema: {type: string}}]
      responses: {"200": {description: OK}}
  /b:
    get: {responses: {"200": {description: OK}}}
`)
	var lines []int
	for _, d := range diags {
		if d.Code == diag.Validation+"/validation-type-mismatch" {
			lines = append(lines, d.Provenance.Position.Line)
		}
	}
	slices.Sort(lines)
	assert.Equal(t, []int{7, 8}, lines, "each value of the wrong type is reported where it is written")
}
