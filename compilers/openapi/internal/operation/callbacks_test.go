package operation_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

const webhookRefSpec = `openapi: 3.1.0
info: {title: T, version: "1"}
webhooks:
  ping: {$ref: '#/components/pathItems/PingItem'}
components:
  pathItems:
    PingItem:
      post: {operationId: onPing, responses: {"200": {description: ok}}}
`

func TestWebhooks_PathItemRefResolved(t *testing.T) {
	t.Parallel()
	doc, _ := parseFull(t, webhookRefSpec)
	op := openapitest.FindOp(t, doc, "onPing")
	require.NotEmpty(t, op.Bindings.HTTP)
	assert.True(t, op.Bindings.HTTP[0].IsWebhook)
	assert.Equal(t, "ping", op.Bindings.HTTP[0].WebhookName,
		"a webhook reached through a $ref'd path item still names its event")
	assert.Empty(t, op.Bindings.HTTP[0].URITemplate)
}

const sharedCallbackSpec = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /c:
    post:
      operationId: parentC
      callbacks:
        onEvent: {$ref: '#/components/callbacks/Shared'}
      responses: {"200": {description: ok}}
  /d:
    post:
      operationId: parentD
      callbacks:
        onEvent: {$ref: '#/components/callbacks/Shared'}
      responses: {"200": {description: ok}}
components:
  callbacks:
    Shared:
      '{$request.body#/url}':
        post:
          operationId: cbPost
          requestBody:
            content:
              application/json:
                schema: {type: object, properties: {n: {type: string}}}
          responses: {"200": {description: ok}}
`

// TestCallbacks_NameWithSlashKeepsItsOwnKey is the encoding case's twin: the
// callback scope is "callbacks/<name>" and the name is a document-chosen map
// key, so a "/" in it must not read as a separator and merge two callbacks into
// one key on the HTTP binding. Both sites take their scope from ids.Scope, and
// each needs its own case, since a fix at one can leave the other standing.
func TestCallbacks_NameWithSlashKeepsItsOwnKey(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.PathsSpec(`  /p:
    post:
      operationId: postP
      responses: {"200": {description: ok}}
      callbacks:
        a:
          x-b/x-c: FROM_A
          '{$request.body#/u}':
            post: {operationId: cbA, responses: {"200": {description: ok}}}
        "a/x-b":
          x-c: FROM_A_SLASH_XB
          '{$request.body#/v}':
            post: {operationId: cbB, responses: {"200": {description: ok}}}
`))
	openapitest.RequireNoErrorDiags(t, diags)
	kept := openapitest.FindOp(t, doc, "postP").Bindings.HTTP[0].Unmodeled

	plain, ok := kept["openapi:callbacks/a/x-b/x-c"]
	require.True(t, ok, `the callback named "a" keeps its own x-b/x-c; got %v`, kept)
	assert.JSONEq(t, `"FROM_A"`, string(plain.Value))

	slashed, ok := kept["openapi:callbacks/a~1x-b/x-c"]
	require.True(t, ok, `the callback named "a/x-b" keeps its own x-c escaped; got %v`, kept)
	assert.JSONEq(t, `"FROM_A_SLASH_XB"`, string(slashed.Value))
}

// TestCallbacks_RefSharedAcrossParentsKeepsDistinctOpIDs is the core scenario
// for a referenced callback (issue #107): two parent operations $ref'ing one
// #/components/callbacks/Shared must keep distinct callback operation
// identities per parent while the callback's own request schema interns once,
// at the shared component's own pointer.
func TestCallbacks_RefSharedAcrossParentsKeepsDistinctOpIDs(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, sharedCallbackSpec)
	openapitest.RequireNoErrorDiags(t, diags)
	parentC := openapitest.FindOp(t, doc, "parentC")
	parentD := openapitest.FindOp(t, doc, "parentD")
	require.Len(t, parentC.Bindings.HTTP[0].Callbacks, 1)
	require.Len(t, parentD.Bindings.HTTP[0].Callbacks, 1)
	require.Len(t, parentC.Bindings.HTTP[0].Callbacks[0].Operations, 1)
	require.Len(t, parentD.Bindings.HTTP[0].Callbacks[0].Operations, 1)

	cbIDC := parentC.Bindings.HTTP[0].Callbacks[0].Operations[0]
	cbIDD := parentD.Bindings.HTTP[0].Callbacks[0].Operations[0]
	assert.NotEqual(t, cbIDC, cbIDD, "callback operations keep distinct identity per parent")
	assert.Equal(t, ir.OpID("op/openapi/paths/~1c/post/callbacks/onEvent/{$request.body#~1url}/post"), cbIDC)
	assert.Equal(t, ir.OpID("op/openapi/paths/~1d/post/callbacks/onEvent/{$request.body#~1url}/post"), cbIDD)

	cbOps := opsByID(t, doc, cbIDC, cbIDD)
	wantReqID := ir.TypeID(
		"t/anon/components/callbacks/Shared/{$request.body#~1url}/post/requestBody/content/application~1json/schema")
	require.NotNil(t, cbOps[cbIDC].Request)
	require.NotNil(t, cbOps[cbIDD].Request)
	assert.Equal(t, wantReqID, cbOps[cbIDC].Request.Contents[0].Type.Target)
	assert.Equal(t, wantReqID, cbOps[cbIDD].Request.Contents[0].Type.Target,
		"the callback's shared request schema interns once")
}

// opsByID collects the operations matching any of ids into a lookup. It
// exists because openapitest.FindOp disambiguates by Name.Source, which two mounts of one
// $ref'd path item or callback share (they are the same declared operationId
// mounted twice); OpID is the only thing that still tells them apart.
func opsByID(t *testing.T, doc *ir.Document, opIDs ...ir.OpID) map[ir.OpID]ir.Operation {
	t.Helper()
	require.NotEmpty(t, doc.Services, "the spec lowers to at least one service")
	want := make(map[ir.OpID]bool, len(opIDs))
	for _, id := range opIDs {
		want[id] = true
	}
	out := make(map[ir.OpID]ir.Operation, len(opIDs))
	for _, g := range doc.Services[0].Groups {
		for _, op := range g.Operations {
			if want[op.ID] {
				out[op.ID] = op
			}
		}
	}
	return out
}

const refdWebhookPathItemSpec = `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
webhooks:
  onA: {$ref: '#/components/pathItems/Hook'}
  onB: {$ref: '#/components/pathItems/Hook'}
components:
  pathItems:
    Hook:
      parameters:
        - {name: shared, in: query, schema: {type: string, enum: [a, b]}}
      post:
        operationId: hookPost
        requestBody:
          content:
            application/json:
              schema: {type: object, properties: {n: {type: string}}}
        responses: {"200": {description: ok}}
`

// TestWebhooks_RefdPathItemSharedAcrossHooksKeepsDistinctOpIDs is the webhook
// arm of issue #107, which the paths arm does not reach: lowerWebhooks resolves
// its own path items, so a component path item mounted as two webhooks must
// keep one operation identity per hook name while its parameter and body
// schemas intern once, at the component's own pointer.
func TestWebhooks_RefdPathItemSharedAcrossHooksKeepsDistinctOpIDs(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, refdWebhookPathItemSpec)
	openapitest.RequireNoErrorDiags(t, diags)

	idA := ir.OpID("op/openapi/webhooks/onA/post")
	idB := ir.OpID("op/openapi/webhooks/onB/post")
	ops := opsByID(t, doc, idA, idB)
	require.Contains(t, ops, idA, "each webhook keeps its own mount identity")
	require.Contains(t, ops, idB, "each webhook keeps its own mount identity")
	assert.True(t, ops[idA].Bindings.HTTP[0].IsWebhook)
	assert.Equal(t, "onA", ops[idA].Bindings.HTTP[0].WebhookName,
		"each mount names its own event")
	assert.Equal(t, "onB", ops[idB].Bindings.HTTP[0].WebhookName)
	assert.Empty(t, ops[idA].Bindings.HTTP[0].URITemplate)
	assert.Empty(t, ops[idB].Bindings.HTTP[0].URITemplate)

	require.Len(t, ops[idA].Params, 1)
	require.Len(t, ops[idB].Params, 1)
	wantParamID := ir.TypeID("t/anon/components/pathItems/Hook/parameters/0/schema")
	assert.Equal(t, wantParamID, ops[idA].Params[0].Type.Target)
	assert.Equal(t, wantParamID, ops[idB].Params[0].Type.Target,
		"the shared webhook path-item parameter interns once")

	require.NotNil(t, ops[idA].Request)
	require.NotNil(t, ops[idB].Request)
	wantReqID := ir.TypeID("t/anon/components/pathItems/Hook/post/requestBody/content/application~1json/schema")
	assert.Equal(t, wantReqID, ops[idA].Request.Contents[0].Type.Target)
	assert.Equal(t, wantReqID, ops[idB].Request.Contents[0].Type.Target,
		"the shared webhook request schema interns once")
}

const refdCallbackPathItemSpec = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /c:
    post:
      operationId: parentC
      callbacks:
        onEvent:
          '{$request.body#/url}': {$ref: '#/components/pathItems/CB'}
      responses: {"200": {description: ok}}
  /d:
    post:
      operationId: parentD
      callbacks:
        onEvent:
          '{$request.body#/url}': {$ref: '#/components/pathItems/CB'}
      responses: {"200": {description: ok}}
components:
  pathItems:
    CB:
      post:
        operationId: cbPost
        requestBody:
          content:
            application/json:
              schema: {type: object, properties: {n: {type: string}}}
        responses: {"200": {description: ok}}
`

// TestCallbacks_RefdPathItemInternsAtDeclaration covers the inner hop of a
// callback: the callback map itself is inline, but the path item under its
// expression is a $ref. That path item is reached through cbDecl rather than
// the parent's mount pointer, so its body must intern at the component while
// the callback operations stay distinct per parent (issue #107).
func TestCallbacks_RefdPathItemInternsAtDeclaration(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, refdCallbackPathItemSpec)
	openapitest.RequireNoErrorDiags(t, diags)
	parentC := openapitest.FindOp(t, doc, "parentC")
	parentD := openapitest.FindOp(t, doc, "parentD")
	require.Len(t, parentC.Bindings.HTTP[0].Callbacks[0].Operations, 1)
	require.Len(t, parentD.Bindings.HTTP[0].Callbacks[0].Operations, 1)

	cbIDC := parentC.Bindings.HTTP[0].Callbacks[0].Operations[0]
	cbIDD := parentD.Bindings.HTTP[0].Callbacks[0].Operations[0]
	assert.NotEqual(t, cbIDC, cbIDD, "callback operations keep distinct identity per parent")

	cbOps := opsByID(t, doc, cbIDC, cbIDD)
	wantReqID := ir.TypeID("t/anon/components/pathItems/CB/post/requestBody/content/application~1json/schema")
	require.NotNil(t, cbOps[cbIDC].Request)
	require.NotNil(t, cbOps[cbIDD].Request)
	assert.Equal(t, wantReqID, cbOps[cbIDC].Request.Contents[0].Type.Target)
	assert.Equal(t, wantReqID, cbOps[cbIDD].Request.Contents[0].Type.Target,
		"the $ref'd callback path item's request schema interns once")
}
