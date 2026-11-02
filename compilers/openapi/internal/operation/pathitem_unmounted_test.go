// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package operation_test

import (
	"encoding/json/jsontext"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/engine/reader"
	"github.com/dexpace/morphic/ir"
)

// unmountedRoutesSpec writes on each of the three routes a path item that
// mounts no operation, each with documentation and a parameter of its own.
const unmountedRoutesSpec = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /p:
    summary: path summary
    description: path description
    parameters:
      - {name: trace, in: header, schema: {type: string}}
      - $ref: '#/components/parameters/Limit'
        description: beside the ref
      - {name: a/b~c, in: query, schema: {type: string}}
  /q:
    post:
      operationId: postQ
      responses: {"200": {description: ok}}
      callbacks:
        onEvent:
          '{$request.body#/url}':
            summary: callback summary
            description: callback description
            parameters:
              - {name: event, in: query, schema: {type: string}}
webhooks:
  hooked:
    summary: webhook summary
    description: webhook description
    parameters:
      - {name: signature, in: header, schema: {type: string}}
components:
  parameters:
    Limit: {name: limit, in: query, schema: {type: integer}}
`

// keptEntry is one Unmodeled entry an item mounting no operation should keep:
// its key, its value as JSON, and the pointer it was written at in the root
// document.
type keptEntry struct{ key, value, at string }

// assertKept holds each want entry to what kept holds under its key, its
// provenance whole, so a position or an inference set on it fails too.
func assertKept(t *testing.T, kept ir.Unmodeled, want []keptEntry) {
	t.Helper()
	for _, w := range want {
		entry, ok := kept[w.key]
		if !assert.True(t, ok, "%s is kept", w.key) {
			continue
		}
		assert.Equal(t, ir.ReasonNoIRHome, entry.Reason, w.key)
		assert.JSONEq(t, w.value, string(entry.Value), w.key)
		assert.Equal(t, ir.Provenance{Pointer: jsontext.Pointer(w.at)}, entry.Provenance, w.key)
	}
}

// TestPathItem_WithNoOperationKeepsItsDocs pins that a path item mounting no
// operation keeps its summary and description beside what else it writes, on
// every route, under its own scope (GitHub #383). Both were dropped with no
// diagnostic, so an item writing nothing else went unannounced too. Each route
// has its own text, so keeping the wrong item's pair fails.
func TestPathItem_WithNoOperationKeepsItsDocs(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, unmountedRoutesSpec)
	openapitest.RequireNoErrorDiags(t, diags)

	const callback = "/paths/~1q/post/callbacks/onEvent/{$request.body#~1url}"
	assertKept(t, doc.Services[0].Unmodeled, []keptEntry{
		{"openapi:pathItem/paths/~1p/summary", `"path summary"`, "/paths/~1p/summary"},
		{"openapi:pathItem/paths/~1p/description", `"path description"`, "/paths/~1p/description"},
		{"openapi:pathItem/webhooks/hooked/summary", `"webhook summary"`, "/webhooks/hooked/summary"},
		{"openapi:pathItem/webhooks/hooked/description", `"webhook description"`, "/webhooks/hooked/description"},
	})
	assertKept(t, openapitest.FindOp(t, doc, "postQ").Bindings.HTTP[0].Unmodeled, []keptEntry{
		{"openapi:pathItem" + callback + "/summary", `"callback summary"`, callback + "/summary"},
		{"openapi:pathItem" + callback + "/description", `"callback description"`, callback + "/description"},
	})
	assert.NotContains(t, doc.Services[0].Unmodeled, "openapi:pathItemSummary",
		"the key an operation keeps the pair under would collide between two items on one carrier")

	for _, d := range diags {
		assert.NotContains(t, d.Message, "path-item documentation kept under Unmodeled",
			"the item's announcement covers its documentation, so it draws no finding of its own")
	}
}

// TestPathItem_WithNoOperationKeepsItsParameters pins that a path item mounting
// no operation keeps its parameters, which lower only by merging into an
// operation, on every route (GitHub #1080). Each entry is kept as written, a
// $ref and the fields beside it included, under the slot its ID would take,
// its name escaped as one token.
func TestPathItem_WithNoOperationKeepsItsParameters(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, unmountedRoutesSpec)
	openapitest.RequireNoErrorDiags(t, diags)

	const callback = "/paths/~1q/post/callbacks/onEvent/{$request.body#~1url}"
	assertKept(t, doc.Services[0].Unmodeled, []keptEntry{
		{"openapi:pathItem/paths/~1p/parameters/trace/header",
			`{"name": "trace", "in": "header", "schema": {"type": "string"}}`, "/paths/~1p/parameters/0"},
		{"openapi:pathItem/paths/~1p/parameters/limit/query",
			`{"$ref": "#/components/parameters/Limit", "description": "beside the ref"}`, "/paths/~1p/parameters/1"},
		{"openapi:pathItem/paths/~1p/parameters/a~1b~0c/query",
			`{"name": "a/b~c", "in": "query", "schema": {"type": "string"}}`, "/paths/~1p/parameters/2"},
		{"openapi:pathItem/webhooks/hooked/parameters/signature/header",
			`{"name": "signature", "in": "header", "schema": {"type": "string"}}`, "/webhooks/hooked/parameters/0"},
	})
	assertKept(t, openapitest.FindOp(t, doc, "postQ").Bindings.HTTP[0].Unmodeled, []keptEntry{
		{"openapi:pathItem" + callback + "/parameters/event/query",
			`{"name": "event", "in": "query", "schema": {"type": "string"}}`, callback + "/parameters/0"},
	})
	assert.Empty(t, openapitest.FindOp(t, doc, "postQ").Params,
		"the callback item's parameter is not taken for the operation declaring the callback")
}

// TestPathItem_WithNoOperationNumbersARepeatedParameter pins that a list
// repeating a slot, which OpenAPI forbids, keeps every entry, each repeat under
// the number its ID would end in, and is reported once, by the load phase.
func TestPathItem_WithNoOperationNumbersARepeatedParameter(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.PathsSpec(`  /p:
    parameters:
      - {name: x, in: query, description: first, schema: {type: string}}
      - {name: x, in: query, description: second, schema: {type: string}}
`))

	assertKept(t, doc.Services[0].Unmodeled, []keptEntry{
		{"openapi:pathItem/paths/~1p/parameters/x/query",
			`{"name": "x", "in": "query", "description": "first", "schema": {"type": "string"}}`, "/paths/~1p/parameters/0"},
		{"openapi:pathItem/paths/~1p/parameters/x/query/2",
			`{"name": "x", "in": "query", "description": "second", "schema": {"type": "string"}}`, "/paths/~1p/parameters/1"},
	})
	var repeats []string
	for _, d := range diags {
		if d.Code == diag.DuplicateParameter {
			repeats = append(repeats, string(d.Provenance.Pointer))
		}
	}
	assert.Equal(t, []string{"/paths/~1p/parameters/1"}, repeats)
}

// TestPathItem_WithNoOperationReportsARepeatInAnotherDocument pins the repeat
// the load phase cannot report: a list another document writes, which its walk
// of the source does not reach. The lowering reports it, at the repeat in that
// document, as it does for an item that mounts an operation.
func TestPathItem_WithNoOperationReportsARepeatInAnotherDocument(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	const other = `X:
  parameters:
    - {name: limit, in: query, schema: {type: integer}}
    - {name: limit, in: query, schema: {type: string}}
`
	const root = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /x: {$ref: "./other.yaml#/X"}
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "other.yaml"), []byte(other), 0o600))
	path := filepath.Join(dir, "root.yaml")
	require.NoError(t, os.WriteFile(path, []byte(root), 0o600))

	doc, diags, err := openapi.New().Compile(t.Context(),
		[]compilers.Source{{Path: path, Data: []byte(root)}},
		compilers.Options{Reader: reader.Disk{}, FormatOptions: openapi.Options{AllowExternalRefs: true}})
	require.NoError(t, err)

	kept := doc.Services[0].Unmodeled
	for key, at := range map[string]string{
		"openapi:pathItem/paths/~1x/parameters/limit/query":   "/X/parameters/0",
		"openapi:pathItem/paths/~1x/parameters/limit/query/2": "/X/parameters/1",
	} {
		require.Contains(t, kept, key)
		assert.Equal(t, ir.Provenance{Source: 1, Pointer: jsontext.Pointer(at)}, kept[key].Provenance, key)
	}
	var repeats []ir.Diagnostic
	for _, d := range diags {
		if d.Code == diag.DuplicateParameter {
			repeats = append(repeats, d)
		}
	}
	require.Len(t, repeats, 1)
	assert.Equal(t, ir.Provenance{Source: 1, Pointer: "/X/parameters/1"}, repeats[0].Provenance)
	assert.Equal(t, `parameter "limit" in "query" repeats the entry at /X/parameters/0`, repeats[0].Message)
}

// TestPathItem_WithNoOperationKeepsNoUnresolvedParameter pins that an entry
// whose $ref resolves to nothing, which has no name and location to key it by,
// is reported once, by the resolver, and kept nowhere, as on an operation.
func TestPathItem_WithNoOperationKeepsNoUnresolvedParameter(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.PathsSpec(`  /p:
    parameters:
      - $ref: '#/components/parameters/Missing'
`))

	assert.Empty(t, doc.Services[0].Unmodeled)
	var unresolved int
	for _, d := range diags {
		if d.Code == diag.UnresolvedRef {
			unresolved++
			assert.Equal(t, jsontext.Pointer("/paths/~1p/parameters/0"), d.Provenance.Pointer)
		}
		assert.NotContains(t, d.Message, "declares no operation this compiler lowers",
			"nothing was kept, so nothing is announced")
	}
	assert.Equal(t, 1, unresolved)
}

// TestPathItem_WithNoOperationKeepsEachMountApart pins the scope: an item two
// paths mount keeps what it writes once per mount, each under that mount's own
// pointer, so neither copy overwrites the other on the one carrier both reach.
func TestPathItem_WithNoOperationKeepsEachMountApart(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a: {$ref: '#/components/pathItems/Reserved'}
  /b: {$ref: '#/components/pathItems/Reserved'}
components:
  pathItems:
    Reserved:
      summary: reserved
      parameters:
        - {name: q, in: query, schema: {type: string}}
`)
	openapitest.RequireNoErrorDiags(t, diags)

	const decl = "/components/pathItems/Reserved"
	const param = `{"name": "q", "in": "query", "schema": {"type": "string"}}`
	assertKept(t, doc.Services[0].Unmodeled, []keptEntry{
		{"openapi:pathItem/paths/~1a/summary", `"reserved"`, decl + "/summary"},
		{"openapi:pathItem/paths/~1a/parameters/q/query", param, decl + "/parameters/0"},
		{"openapi:pathItem/paths/~1b/summary", `"reserved"`, decl + "/summary"},
		{"openapi:pathItem/paths/~1b/parameters/q/query", param, decl + "/parameters/0"},
	})
}
