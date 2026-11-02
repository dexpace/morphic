// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package operation_test

import (
	"encoding/json/jsontext"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irverify"
)

// pathItemRefSiblingsSpec is the issue's own reproducer: a path whose whole
// entry is a $ref to another path, with a summary and an operation written
// beside the reference (GitHub #577).
const pathItemRefSiblingsSpec = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a:
    $ref: '#/paths/~1b'
    summary: written beside the ref
    put:
      operationId: putA
      responses: {"200": {description: OK}}
  /b:
    get:
      operationId: getB
      responses: {"200": {description: OK}}
`

// TestPathItemRefSiblings_IssueReproducer pins the whole fix at the shape the
// issue reported: the operation written beside the $ref reaches the IR at its
// own mount pointer, the sibling's summary is kept, and the referenced item's
// own operation still mounts where the document writes it.
func TestPathItemRefSiblings_IssueReproducer(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, pathItemRefSiblingsSpec)
	openapitest.RequireNoErrorDiags(t, diags)

	putA := openapitest.FindOp(t, doc, "putA")
	assert.Equal(t, ir.OpID("op/openapi/paths/~1a/put"), putA.ID)
	assert.Equal(t, jsontext.Pointer("/paths/~1a/put"), putA.Provenance.Pointer,
		"the sibling operation is declared where it is mounted")
	assert.Equal(t, "PUT", putA.Bindings.HTTP[0].Method)
	assert.Equal(t, "/a", putA.Bindings.HTTP[0].URITemplate)

	entry, ok := putA.Unmodeled["openapi:pathItem/summary"]
	require.True(t, ok, "the summary written beside the $ref is kept")
	assert.JSONEq(t, `"written beside the ref"`, string(entry.Value))
	assert.Equal(t, jsontext.Pointer("/paths/~1a/summary"), entry.Provenance.Pointer)

	// The referent's own operation keeps mounting at both paths: a use site that
	// adds fields beside the $ref does not stop referencing it.
	atA := opByPath(t, doc, "GET", "/a")
	atB := opByPath(t, doc, "GET", "/b")
	assert.Equal(t, ir.OpID("op/openapi/paths/~1a/get"), atA.ID)
	assert.Equal(t, ir.OpID("op/openapi/paths/~1b/get"), atB.ID)
	assert.Equal(t, jsontext.Pointer("/paths/~1b/get"), atA.Provenance.Pointer,
		"the remounted operation is still declared at the component it names")
	assert.Equal(t, "getB", atA.Name.Source)

	// And the sibling's item-level constructs land on every operation mounted
	// at that path, the referent's included.
	assert.Contains(t, atA.Unmodeled, "openapi:pathItem/summary")
	assert.True(t, openapitest.HasDiagCodeAt(diags, diag.DuplicateOperationID, "/paths/~1a/get"),
		"one declaration mounted twice is still the remount warning, unchanged")
	assert.False(t, openapitest.HasDiag(diags, diag.ConflictingOperationID))
}

// pathItemRefWebhookSpec is the same shape under webhooks, which is the second
// route a path item is reached through, carrying the same item-level construct
// set the paths route's fixture does.
const pathItemRefWebhookSpec = `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
webhooks:
  hooked:
    $ref: '#/components/pathItems/Shared'
    summary: written beside the ref
    servers: [{url: https://use.example}]
    x-beside: use
    bogusUse: {responses: {"200": {description: use}}}
    parameters:
      - {name: useParam, in: query, schema: {type: string}}
    post:
      operationId: siblingHook
      responses: {"200": {description: OK}}
components:
  pathItems:
    Shared:
      get:
        operationId: refHook
        responses: {"200": {description: OK}}
`

// TestPathItemRefSiblings_WebhookRoute holds the webhook route to the paths
// route's reading: the same model and the same carriers, reached under webhooks
// rather than paths. Every item-level construct the paths route asserts lands
// on every operation mounted here, under the hook's own use site.
func TestPathItemRefSiblings_WebhookRoute(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, pathItemRefWebhookSpec)
	openapitest.RequireNoErrorDiags(t, diags)

	sibling := openapitest.FindOp(t, doc, "siblingHook")
	assert.Equal(t, ir.OpID("op/openapi/webhooks/hooked/post"), sibling.ID)
	assert.Equal(t, jsontext.Pointer("/webhooks/hooked/post"), sibling.Provenance.Pointer)
	assert.True(t, sibling.Bindings.HTTP[0].IsWebhook, "a webhook operation keeps its binding")

	referent := openapitest.FindOp(t, doc, "refHook")
	assert.Equal(t, ir.OpID("op/openapi/webhooks/hooked/get"), referent.ID)

	for _, op := range []ir.Operation{sibling, referent} {
		assertPathItemSiblingConstructs(t, doc, op, "/webhooks/hooked")
	}
	assert.True(t, openapitest.HasDiagCodeAt(diags, diag.UnknownObjectKey, "/webhooks/hooked/bogusUse"),
		"and the census names the use site's undeclared key where it was written")
}

// pathItemRefCallbackSpec is the same shape at a callback expression, the third
// route a path item is reached through, carrying the same item-level construct
// set the paths route's fixture does.
const pathItemRefCallbackSpec = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /p:
    post:
      operationId: parent
      callbacks:
        onEvent:
          '{$request.body#/url}':
            $ref: '#/components/pathItems/Cb'
            summary: written beside the ref
            servers: [{url: https://use.example}]
            x-beside: use
            bogusUse: {responses: {"200": {description: use}}}
            parameters:
              - {name: useParam, in: query, schema: {type: string}}
            post:
              operationId: siblingCb
              responses: {"200": {description: OK}}
      responses: {"200": {description: OK}}
components:
  pathItems:
    Cb:
      get:
        operationId: refCb
        responses: {"200": {description: OK}}
`

// TestPathItemRefSiblings_CallbackRoute holds the callback route to the paths
// route's reading: every item-level construct lands on every operation mounted
// at the expression, under the expression's own use site.
func TestPathItemRefSiblings_CallbackRoute(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, pathItemRefCallbackSpec)
	openapitest.RequireNoErrorDiags(t, diags)

	const usePtr = "/paths/~1p/post/callbacks/onEvent/{$request.body#~1url}"

	sibling := openapitest.FindOp(t, doc, "siblingCb")
	assert.Equal(t, jsontext.Pointer(usePtr+"/post"), sibling.Provenance.Pointer)

	referent := openapitest.FindOp(t, doc, "refCb")
	assert.Equal(t, jsontext.Pointer("/components/pathItems/Cb/get"), referent.Provenance.Pointer)

	for _, op := range []ir.Operation{sibling, referent} {
		assertPathItemSiblingConstructs(t, doc, op, usePtr)
	}
	assert.True(t, openapitest.HasDiagCodeAt(diags, diag.UnknownObjectKey, usePtr+"/bogusUse"),
		"and the census names the use site's undeclared key where it was written")
}

// pathItemRefItemSpec declares item-level constructs beside a $ref: servers, an
// x-* extension, an undeclared key and an item-level parameter, none of which
// the reference wrapper models.
const pathItemRefItemSpec = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a:
    $ref: '#/paths/~1b'
    summary: written beside the ref
    servers: [{url: https://use.example}]
    x-beside: use
    bogusUse: {responses: {"200": {description: use}}}
    parameters:
      - {name: useParam, in: query, schema: {type: string}}
    put:
      operationId: usePut
      responses: {"200": {description: OK}}
  /b:
    get:
      operationId: getB
      responses: {"200": {description: OK}}
`

// TestPathItemRefSiblings_ItemConstructsReachEveryOperation pins that what the
// use site writes beside the $ref reaches the IR on every operation mounted at
// that path, each under its own pointer, exactly as a path item's own
// constructs do: the referenced item's operation is asserted beside the one
// written next to the reference.
func TestPathItemRefSiblings_ItemConstructsReachEveryOperation(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, pathItemRefItemSpec)
	openapitest.RequireNoErrorDiags(t, diags)

	mounted := []ir.Operation{
		opByPath(t, doc, "GET", "/a"),        // the referent's, mounted at this path
		openapitest.FindOp(t, doc, "usePut"), // the use site's own
	}
	for _, op := range mounted {
		assertPathItemSiblingConstructs(t, doc, op, "/paths/~1a")
	}
	assert.True(t, openapitest.HasDiagCodeAt(diags, diag.UnknownObjectKey, "/paths/~1a/bogusUse"),
		"and the census names it where it was written")
}

// assertPathItemSiblingConstructs requires the full item-level construct set a
// use site writes beside a $ref to have reached op, each under the use site's
// own pointer: the summary, the servers, an x-* extension, an undeclared key,
// and the item parameter drawn from the use site's YAML.
//
// It is shared by the paths, webhook and callback route tests so the three
// cannot assert different constructs: what one route forgets is what all three
// fail to notice, which is exactly how the servers half came to be missing on
// two of the three routes (GitHub #39).
func assertPathItemSiblingConstructs(t *testing.T, doc *ir.Document, op ir.Operation, usePtr string) {
	t.Helper()
	where := op.Name.Source

	summary, ok := op.Unmodeled["openapi:pathItem/summary"]
	require.True(t, ok, "%s keeps the use site's path-item summary", where)
	assert.JSONEq(t, `"written beside the ref"`, string(summary.Value))
	assert.Equal(t, jsontext.Pointer(usePtr+"/summary"), summary.Provenance.Pointer)

	assert.Equal(t, []string{"https://use.example"}, serversNamed(t, doc, op.Servers),
		"%s is sent to the use site's servers", where)
	assert.Equal(t, []ir.ServerID{ir.ServerID("server/openapi" + usePtr + "/servers/0")}, op.Servers,
		"%s names them where the use site declares them", where)

	ext, ok := op.Unmodeled["openapi:pathItem/x-beside"]
	require.True(t, ok, "%s keeps the use site's x-*", where)
	assert.JSONEq(t, `"use"`, string(ext.Value))
	assert.Equal(t, jsontext.Pointer(usePtr+"/x-beside"), ext.Provenance.Pointer)

	unknown, ok := op.Unmodeled["openapi:pathItem/bogusUse"]
	require.True(t, ok, "%s keeps the use site's undeclared key", where)
	assert.JSONEq(t, `{"responses":{"200":{"description":"use"}}}`, string(unknown.Value))
	assert.Equal(t, jsontext.Pointer(usePtr+"/bogusUse"), unknown.Provenance.Pointer)

	require.Len(t, op.Params, 1, "%s merges the use site's item parameter", where)
	assert.Equal(t, "useParam", op.Params[0].Name.Source)
	assert.Equal(t, jsontext.Pointer(usePtr+"/parameters/0"), op.Params[0].Provenance.Pointer)
}

// pathItemRefSharedCallbackSpec has two operations share one $ref'd callback
// whose expression writes an operation beside its $ref, and a components path
// item carrying such a callback inline mounted at a path and a webhook.
const pathItemRefSharedCallbackSpec = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /one:
    post:
      callbacks: {cb: {$ref: '#/components/callbacks/CB'}}
      responses: {"200": {description: OK}}
  /two:
    post:
      callbacks: {cb: {$ref: '#/components/callbacks/CB'}}
      responses: {"200": {description: OK}}
  /three: {$ref: '#/components/pathItems/Inline'}
webhooks:
  hook: {$ref: '#/components/pathItems/Inline'}
components:
  pathItems:
    Target:
      post: {responses: {"200": {description: OK}}}
    Inline:
      post:
        callbacks:
          cb:
            '{$request.body#/url}':
              $ref: '#/components/pathItems/Target'
              put: {responses: {"200": {description: OK}}}
        responses: {"200": {description: OK}}
  callbacks:
    CB:
      '{$request.body#/url}':
        $ref: '#/components/pathItems/Target'
        put: {responses: {"200": {description: OK}}}
`

// TestPathItemRefSiblings_ASharedCallbackMountsPerParent pins the identity of an
// operation written beside a callback expression's $ref: it mounts under each
// parent, as the referenced item's own do, and is declared once, where the
// expression writes it. Mounted at the declaration, every parent sharing the
// callback produced one operation ID.
func TestPathItemRefSiblings_ASharedCallbackMountsPerParent(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, pathItemRefSharedCallbackSpec)
	openapitest.RequireNoErrorDiags(t, diags)
	assert.Empty(t, irverify.Verify(doc), "no operation ID is declared twice")

	const expr = "/callbacks/cb/{$request.body#~1url}/put"
	puts := map[ir.OpID]jsontext.Pointer{}
	for _, g := range doc.Services[0].Groups {
		for _, op := range g.Operations {
			if strings.HasSuffix(string(op.ID), expr) {
				puts[op.ID] = op.Provenance.Pointer
			}
		}
	}
	assert.Equal(t, map[ir.OpID]jsontext.Pointer{
		"op/openapi/paths/~1one/post" + expr:   "/components/callbacks/CB/{$request.body#~1url}/put",
		"op/openapi/paths/~1two/post" + expr:   "/components/callbacks/CB/{$request.body#~1url}/put",
		"op/openapi/paths/~1three/post" + expr: "/components/pathItems/Inline/post/callbacks/cb/{$request.body#~1url}/put",
		"op/openapi/webhooks/hook/post" + expr: "/components/pathItems/Inline/post/callbacks/cb/{$request.body#~1url}/put",
	}, puts, "one operation per parent, each declared where the expression writes it")
}

// pathItemRefOrderSpec mounts /b's operations at /a and adds its own beside the
// $ref, one of them a repeat of /b's COPY. /c writes every operation /a ends up
// with inline, in the order /b and /a write them.
const pathItemRefOrderSpec = `openapi: 3.2.0
info: {title: T, version: "1"}
paths:
  /a:
    $ref: '#/paths/~1b'
    put: {operationId: putA, responses: {"200": {description: OK}}}
    additionalOperations:
      COPY: {operationId: copyA, responses: {"200": {description: OK}}}
      LINK: {operationId: linkA, responses: {"200": {description: OK}}}
  /b:
    post: {operationId: postB, responses: {"200": {description: OK}}}
    get: {operationId: getB, responses: {"200": {description: OK}}}
    additionalOperations:
      LOCK: {operationId: lockB, responses: {"200": {description: OK}}}
      COPY: {operationId: copyB, responses: {"200": {description: OK}}}
  /c:
    post: {responses: {"200": {description: OK}}}
    put: {responses: {"200": {description: OK}}}
    get: {responses: {"200": {description: OK}}}
    additionalOperations:
      LOCK: {responses: {"200": {description: OK}}}
      COPY: {responses: {"200": {description: OK}}}
      LINK: {responses: {"200": {description: OK}}}
`

// TestPathItemRefSiblings_MountsInTheOrderAnInlineItemDoes pins the order a
// mount lists its layers' operations in: the order the same operations written
// inline get, whichever layer declares each. Listing layer by layer put /a's PUT
// after all of /b's, and taking the additional keys nearest layer first put /a's
// LINK before /b's LOCK.
func TestPathItemRefSiblings_MountsInTheOrderAnInlineItemDoes(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, pathItemRefOrderSpec)
	openapitest.RequireNoErrorDiags(t, diags)

	inline := methodsAt(doc, "/c")
	require.Equal(t, []string{"GET", "PUT", "POST", "LOCK", "COPY", "LINK"}, inline)
	assert.Equal(t, inline, methodsAt(doc, "/a"), "the mount lists them as /c, written inline, does")
	assert.Equal(t, "copyA", opByPath(t, doc, "COPY", "/a").Name.Source,
		"the repeated key keeps its place and takes the nearer layer's declaration")
}

// methodsAt returns the methods of the operations mounted at uri, in the order
// the document's groups list them.
func methodsAt(doc *ir.Document, uri string) []string {
	var methods []string
	for _, g := range doc.Services[0].Groups {
		for _, op := range g.Operations {
			if hb := op.Bindings.HTTP[0]; hb.URITemplate == uri {
				methods = append(methods, hb.Method)
			}
		}
	}
	return methods
}
