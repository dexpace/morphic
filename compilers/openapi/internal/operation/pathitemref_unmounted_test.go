// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package operation_test

import (
	"encoding/json/jsontext"
	"os"
	"path/filepath"
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
)

// pathItemRefUnmountedSpec mounts no operation on either side of the reference:
// the use site writes item-level constructs and the referent only a summary.
const pathItemRefUnmountedSpec = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a:
    $ref: '#/paths/~1b'
    servers: [{url: https://use.example}]
    x-beside: use
  /b:
    summary: ref summary
`

// TestPathItemRefSiblings_UnmountedKeepsSiblings pins the third carrier: an item
// that mounts no operation on either side has no operation to hold the use
// site's constructs, so they are kept on the nearest node that has an Unmodeled
// map, its servers registered, and announced, exactly as a path item with no
// operations already was.
func TestPathItemRefSiblings_UnmountedKeepsSiblings(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, pathItemRefUnmountedSpec)
	openapitest.RequireNoErrorDiags(t, diags)

	svc := doc.Services[0]
	const use = ir.ServerID("server/openapi/paths/~1a/servers/0")
	require.Contains(t, doc.Servers, use, "the use site's servers are registered")
	assert.Equal(t, "https://use.example", doc.Servers[use].URLTemplate)
	assert.NotContains(t, svc.Servers, use, "and serve nothing, the service included")
	ext, ok := svc.Unmodeled["openapi:pathItem/paths/~1a/x-beside"]
	require.True(t, ok)
	assert.JSONEq(t, `"use"`, string(ext.Value))
	assert.True(t, openapitest.HasDiagCodeAt(diags, diag.DegradedConstruct, "/paths/~1a"),
		"the preservation is announced at the use site's own pointer")
}

// TestPathItemRefSiblings_UnmountedSaysOnceWhatEveryLayerKept pins that a
// mount producing no operation is announced once, at its use site, naming what
// any of its layers landed: the use site wrote an extension and the referent
// the servers, and one finding says both.
func TestPathItemRefSiblings_UnmountedSaysOnceWhatEveryLayerKept(t *testing.T) {
	t.Parallel()
	_, diags := parseFull(t, `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a:
    $ref: '#/paths/~1b'
    x-beside: use
  /b:
    servers: [{url: https://far.example}]
`)
	openapitest.RequireNoErrorDiags(t, diags)

	var said []string
	for _, d := range diags {
		if d.Code == diag.DegradedConstruct && d.Provenance.Pointer == "/paths/~1a" {
			said = append(said, strings.TrimPrefix(d.Message, "path item declares no operation this compiler lowers; "))
		}
	}
	assert.Equal(t, []string{
		"its servers are registered, named by no operation, and what else it writes is kept beside it, " +
			"having no operation to hold it",
	}, said)
}

// pathItemRefUnmountedWebhookSpec mounts nothing on either side of a webhook's
// $ref, so the use site's constructs have no operation to ride on.
const pathItemRefUnmountedWebhookSpec = `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
webhooks:
  hooked:
    $ref: '#/components/pathItems/Empty'
    x-beside: use
components:
  pathItems:
    Empty:
      summary: ref summary
`

// TestPathItemRefSiblings_UnmountedWebhookKeepsSiblings pins the orphan carrier
// on the webhook route: the use site's constructs land on the service, under the
// use site's own scope, and the preservation is announced there.
func TestPathItemRefSiblings_UnmountedWebhookKeepsSiblings(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, pathItemRefUnmountedWebhookSpec)
	openapitest.RequireNoErrorDiags(t, diags)

	entry, ok := doc.Services[0].Unmodeled["openapi:pathItem/webhooks/hooked/x-beside"]
	require.True(t, ok, "the webhook's sibling x-* is kept on the service")
	assert.JSONEq(t, `"use"`, string(entry.Value))
	assert.True(t, openapitest.HasDiagCodeAt(diags, diag.DegradedConstruct, "/webhooks/hooked"),
		"the preservation is announced at the use site's own pointer")
}

// pathItemRefUnmountedCallbackSpec mounts nothing on either side of a callback
// expression's $ref.
const pathItemRefUnmountedCallbackSpec = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /p:
    post:
      operationId: parent
      callbacks:
        onEvent:
          '{$request.body#/url}':
            $ref: '#/components/pathItems/Empty'
            x-beside: use
      responses: {"200": {description: OK}}
components:
  pathItems:
    Empty:
      summary: ref summary
`

// TestPathItemRefSiblings_UnmountedCallbackKeepsSiblings pins the orphan carrier
// on the callback route, which puts the kept constructs on the parent's HTTP
// binding, where the callback itself lives.
func TestPathItemRefSiblings_UnmountedCallbackKeepsSiblings(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, pathItemRefUnmountedCallbackSpec)
	openapitest.RequireNoErrorDiags(t, diags)

	const expr = "{$request.body#~1url}"
	parent := openapitest.FindOp(t, doc, "parent")
	entry, ok := parent.Bindings.HTTP[0].Unmodeled["openapi:pathItem/paths/~1p/post/callbacks/onEvent/"+expr+"/x-beside"]
	require.True(t, ok, "the expression's sibling x-* is kept on the parent's binding")
	assert.JSONEq(t, `"use"`, string(entry.Value))
	assert.True(t, openapitest.HasDiagCodeAt(diags, diag.DegradedConstruct,
		"/paths/~1p/post/callbacks/onEvent/"+expr),
		"the preservation is announced at the use site's own pointer")
}

// pathItemRefUnmountedCollisionSpec declares the same constructs on both sides
// of a $ref neither side mounts an operation for, and two on the referent
// alone.
const pathItemRefUnmountedCollisionSpec = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a:
    $ref: '#/paths/~1b'
    summary: use summary
    x-both: use
    bogusBoth: {responses: {"200": {description: USE}}}
    parameters:
      - {name: both, in: query, description: USE, schema: {type: string}}
  /b:
    summary: ref summary
    description: ref description
    x-both: ref
    bogusBoth: {responses: {"200": {description: REF}}}
    parameters:
      - {name: both, in: query, description: REF, schema: {type: string}}
      - {name: far, in: query, description: REF, schema: {type: string}}
`

// TestPathItemRefSiblings_UnmountedCollisionKeepsTheUseSite pins precedence on
// the carrier an item mounting no operation uses: the use site keeps every
// construct both sides declare, its summary and its parameters included, and
// the referent keeps what the use site does not declare, each at its own
// pointer.
func TestPathItemRefSiblings_UnmountedCollisionKeepsTheUseSite(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, pathItemRefUnmountedCollisionSpec)
	openapitest.RequireNoErrorDiags(t, diags)

	svc := doc.Services[0]
	for key, want := range map[string]struct{ value, at string }{
		"openapi:pathItem/paths/~1a/x-both":    {`"use"`, "/paths/~1a/x-both"},
		"openapi:pathItem/paths/~1a/bogusBoth": {`{"responses":{"200":{"description":"USE"}}}`, "/paths/~1a/bogusBoth"},
		"openapi:pathItem/paths/~1a/summary":   {`"use summary"`, "/paths/~1a/summary"},
		"openapi:pathItem/paths/~1a/parameters/both/query": {
			`{"name":"both","in":"query","description":"USE","schema":{"type":"string"}}`, "/paths/~1a/parameters/0"},
		"openapi:pathItem/paths/~1a/description": {`"ref description"`, "/paths/~1b/description"},
		"openapi:pathItem/paths/~1a/parameters/far/query": {
			`{"name":"far","in":"query","description":"REF","schema":{"type":"string"}}`, "/paths/~1b/parameters/1"},
	} {
		entry, ok := svc.Unmodeled[key]
		require.True(t, ok, key)
		assert.JSONEq(t, want.value, string(entry.Value), key)
		assert.Equal(t, ir.Provenance{Pointer: jsontext.Pointer(want.at)}, entry.Provenance, key)
	}
	for _, at := range []string{"x-both", "bogusBoth", "summary", "parameters/0"} {
		assert.True(t, openapitest.HasDiagCodeAt(diags, diag.PathItemRefCollision, "/paths/~1a/"+at), at)
	}
	assert.False(t, openapitest.HasDiag(diags, diag.UnknownKeyEntryTaken))
}

// TestPathItem_WithNoOperationIsReportedAtItsUseSite pins where a mount
// producing no operation is announced when the item is another document's: at
// the use site writing it, in the document writing it, for a path, and for a
// callback expression, whose use site is where its Callback Object writes it.
// Neither the mount's pointer nor the item's document holds the position a
// finding named there.
func TestPathItem_WithNoOperationIsReportedAtItsUseSite(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "other.yaml"), []byte(`openapi: 3.1.0
info: {title: O, version: "1"}
paths: {}
components:
  pathItems:
    P:
      x-far: 1
  callbacks:
    C:
      '{$request.body#/u}': {$ref: '#/components/pathItems/P'}
`), 0o600))
	doc, diags, err := openapi.New().Compile(t.Context(),
		[]compilers.Source{{Path: filepath.Join(dir, "root.yaml"), Data: []byte(`openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a:
    $ref: './other.yaml#/components/pathItems/P'
    x-near: 1
  /b:
    post:
      responses: {"200": {description: ok}}
      callbacks:
        cb: {$ref: './other.yaml#/components/callbacks/C'}
`)}}, compilers.Options{Reader: reader.Disk{}, FormatOptions: openapi.Options{AllowExternalRefs: true}})
	require.NoError(t, err)
	require.NotNil(t, doc)

	var found []string
	for _, d := range diags {
		if d.Code == diag.DegradedConstruct {
			require.Less(t, d.Provenance.Source, len(doc.Sources))
			found = append(found, filepath.Base(doc.Sources[d.Provenance.Source].Path)+"#"+string(d.Provenance.Pointer))
		}
	}
	assert.Equal(t, []string{"root.yaml#/paths/~1a", "other.yaml#/components/callbacks/C/{$request.body#~1u}"}, found)
}
