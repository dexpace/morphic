package operation_test

import (
	"encoding/json/jsontext"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/engine/reader"
	"github.com/dexpace/morphic/ir"
)

func TestOperation_ExplicitlyPublicSecurity(t *testing.T) {
	t.Parallel()
	spec := openapitest.PathsSpec(`  /open:
    get:
      operationId: open
      security: []
      responses: {"200": {description: ok}}
  /inherits:
    get:
      operationId: inherits
      responses: {"200": {description: ok}}
`)
	_, svc, diags := lowerServiceSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	ops := map[string]ir.Operation{}
	for _, g := range svc.Groups {
		for _, op := range g.Operations {
			ops[op.Name.Source] = op
		}
	}
	require.NotNil(t, ops["open"].Auth, "security: [] must be the empty non-nil slice")
	assert.Empty(t, ops["open"].Auth)
	assert.Nil(t, ops["inherits"].Auth, "absent security inherits the service default")
}

// TestParameters_PathItemSharedAcrossOperationsInternsOnce is the fix's core
// scenario (issue #36): a path-item-level parameter merged into two sibling
// operations must intern its schema once, at the path item's own pointer —
// not once per operation under a pointer fabricated from that operation's
// unrelated parameter count.
func TestParameters_PathItemSharedAcrossOperationsInternsOnce(t *testing.T) {
	t.Parallel()
	spec := openapitest.PathsSpec(`  /pets/{petId}:
    parameters:
      - name: petId
        in: path
        required: true
        schema: {type: string, enum: [a, b]}
    get:
      operationId: getPet
      responses: {"200": {description: ok}}
    delete:
      operationId: deletePet
      parameters:
        - {name: force, in: query, schema: {type: boolean}}
      responses: {"200": {description: ok}}
`)
	doc, _, diags := lowerServiceSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	getPet := openapitest.FindOp(t, doc, "getPet")
	deletePet := openapitest.FindOp(t, doc, "deletePet")
	require.Len(t, getPet.Params, 1, "get inherits only the shared path-item parameter")
	require.Len(t, deletePet.Params, 2, "delete gets its own force plus the shared path-item parameter")

	wantID := ir.TypeID("t/anon/paths/~1pets~1{petId}/parameters/0/schema")
	assert.Equal(t, wantID, getPet.Params[0].Type.Target, "get resolves the shared path-item schema")

	byName := openapitest.IndexBy(deletePet.Params, func(p ir.Parameter) string { return p.Name.Source })
	assert.Equal(t, wantID, byName["petId"].Type.Target, "delete resolves the same shared schema, not a copy")

	typeDef, ok := doc.Types[wantID]
	require.True(t, ok, "the shared schema is registered under the path item's own pointer")
	assert.Equal(t, jsontext.Pointer("/paths/~1pets~1{petId}/parameters/0/schema"), typeDef.Common().Provenance.Pointer)

	_, fabricatedGet := doc.Types[ir.TypeID("t/anon/paths/~1pets~1{petId}/get/parameters/0/schema")]
	_, fabricatedDelete := doc.Types[ir.TypeID("t/anon/paths/~1pets~1{petId}/delete/parameters/1/schema")]
	assert.False(t, fabricatedGet, "no fabricated per-operation ID for get")
	assert.False(t, fabricatedDelete, "no fabricated per-operation ID for delete")
}

// TestParameters_PathItemSchemaIDStableAcrossOperationParamChanges guards the
// specific regression from issue #36: adding an unrelated operation-level
// parameter must not shift the TypeID already handed out for a sibling
// path-item-level parameter's schema.
func TestParameters_PathItemSchemaIDStableAcrossOperationParamChanges(t *testing.T) {
	t.Parallel()
	before := openapitest.PathsSpec(`  /pets/{petId}:
    parameters:
      - name: petId
        in: path
        required: true
        schema: {type: string, enum: [a, b]}
    get:
      operationId: getPet
      responses: {"200": {description: ok}}
`)
	after := openapitest.PathsSpec(`  /pets/{petId}:
    parameters:
      - name: petId
        in: path
        required: true
        schema: {type: string, enum: [a, b]}
    get:
      operationId: getPet
      parameters:
        - {name: verbose, in: query, schema: {type: boolean}}
      responses: {"200": {description: ok}}
`)
	_, svcBefore, diagsBefore := lowerServiceSpec(t, before)
	openapitest.RequireNoErrorDiags(t, diagsBefore)
	_, svcAfter, diagsAfter := lowerServiceSpec(t, after)
	openapitest.RequireNoErrorDiags(t, diagsAfter)

	opBefore := openapitest.FirstOp(t, svcBefore)
	opAfter := openapitest.FirstOp(t, svcAfter)
	require.Len(t, opBefore.Params, 1)
	require.Len(t, opAfter.Params, 2, "the unrelated verbose parameter adds a second entry")

	byName := openapitest.IndexBy(opAfter.Params, func(p ir.Parameter) string { return p.Name.Source })
	assert.Equal(t, opBefore.Params[0].Type.Target, byName["petId"].Type.Target,
		"an unrelated operation-level parameter must not shift the shared path-item schema's ID")
}

// TestParameters_WebhookPathItemParameterPointer covers the same merge through
// lowerWebhooks: a webhook path-item-level parameter must hoist at the webhook
// path item's own pointer.
func TestParameters_WebhookPathItemParameterPointer(t *testing.T) {
	t.Parallel()
	spec := `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
webhooks:
  petEvent:
    parameters:
      - name: kind
        in: query
        schema: {type: string, enum: [created, deleted]}
    post:
      operationId: onPetEvent
      responses: {"200": {description: ok}}
`
	doc, _, diags := lowerServiceSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	op := openapitest.FindOp(t, doc, "onPetEvent")
	require.Len(t, op.Params, 1)

	wantID := ir.TypeID("t/anon/webhooks/petEvent/parameters/0/schema")
	assert.Equal(t, wantID, op.Params[0].Type.Target)
	typeDef, ok := doc.Types[wantID]
	require.True(t, ok)
	assert.Equal(t, jsontext.Pointer("/webhooks/petEvent/parameters/0/schema"), typeDef.Common().Provenance.Pointer)
}

// TestParameters_CallbackPathItemParameterPointer covers the same merge
// through lowerCallbackOps: a callback path-item-level parameter must hoist at
// the callback path item's own pointer, nested under the parent operation and
// callback expression.
func TestParameters_CallbackPathItemParameterPointer(t *testing.T) {
	t.Parallel()
	spec := openapitest.PathsSpec(`  /subscribe:
    post:
      operationId: subscribe
      callbacks:
        onEvent:
          '{$request.body#/callbackUrl}':
            parameters:
              - name: token
                in: query
                schema: {type: string, enum: [a, b]}
            post:
              operationId: onEvent
              responses: {"200": {description: ok}}
      responses: {"200": {description: ok}}
`)
	doc, _, diags := lowerServiceSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	cbOp := openapitest.FindOp(t, doc, "onEvent")
	require.Len(t, cbOp.Params, 1)

	wantPointer := "/paths/~1subscribe/post/callbacks/onEvent/{$request.body#~1callbackUrl}/parameters/0/schema"
	wantID := ir.TypeID("t/anon" + wantPointer)
	assert.Equal(t, wantID, cbOp.Params[0].Type.Target)
	typeDef, ok := doc.Types[wantID]
	require.True(t, ok)
	assert.Equal(t, jsontext.Pointer(wantPointer), typeDef.Common().Provenance.Pointer)
}

// TestParameters_ShadowedPathItemParamUsesOperationPointer covers the other
// side of the merge: when the operation redeclares a path-item parameter, the
// operation's own schema must be used and lowered at the operation's own
// pointer — the shadowed path-item schema is never lowered at all.
func TestParameters_ShadowedPathItemParamUsesOperationPointer(t *testing.T) {
	t.Parallel()
	spec := openapitest.PathsSpec(`  /pets/{petId}:
    parameters:
      - name: petId
        in: path
        required: true
        schema: {type: string, enum: [a, b]}
    get:
      operationId: getPet
      parameters:
        - name: petId
          in: path
          required: true
          schema: {type: string, enum: [c, d]}
      responses: {"200": {description: ok}}
`)
	doc, svc, diags := lowerServiceSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	op := openapitest.FirstOp(t, svc)
	require.Len(t, op.Params, 1, "the operation parameter shadows the path-item one; no duplicate")

	wantID := ir.TypeID("t/anon/paths/~1pets~1{petId}/get/parameters/0/schema")
	assert.Equal(t, wantID, op.Params[0].Type.Target, "the operation's own schema wins, at the operation's pointer")

	enum, ok := doc.Types[wantID].(*ir.Enum)
	require.True(t, ok)
	require.Len(t, enum.Members, 2)
	assert.Equal(t, "c", enum.Members[0].Value.Str, "the operation-level schema's own values are used")

	_, atPathLevel := doc.Types[ir.TypeID("t/anon/paths/~1pets~1{petId}/parameters/0/schema")]
	assert.False(t, atPathLevel, "the shadowed path-item schema is never lowered")
}

func TestOperation_NoOperationIdHint(t *testing.T) {
	t.Parallel()
	spec := openapitest.PathsSpec(`  /ping:
    get:
      responses: {"200": {description: ok}}
`)
	_, svc, diags := lowerServiceSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	op := openapitest.FirstOp(t, svc)
	assert.Empty(t, op.Name.Source, "no operationId leaves an empty source name")
	assert.Equal(t, "get_ping", op.Name.Hint, "the hint is canonicalized, not the raw method and template")
}

const opsSpec = `openapi: 3.1.0
info: {title: T, version: "1"}
servers:
  - url: https://api.example.com
tags:
  - name: main
    description: Main
    externalDocs: {url: 'https://docs', description: d}
paths:
  /a:
    servers:
      - url: https://a.example.com
    get:
      operationId: getA
      tags: [ghost]
      externalDocs: {url: 'https://x'}
      x-flag: true
      parameters:
        - {$ref: '#/components/parameters/PageParam'}
      responses:
        "200":
          description: ok
          headers:
            X-Rate: {$ref: '#/components/headers/RateLimit'}
        "404": {$ref: '#/components/responses/NotFound'}
        "4XX": {description: client range}
        "5XX": {description: server range}
    put: {operationId: putA, responses: {"200": {description: ok}}}
    post:
      operationId: postA
      callbacks:
        onEvent: {$ref: '#/components/callbacks/OnEvent'}
      responses: {"200": {description: ok}}
    delete: {operationId: delA, responses: {"200": {description: ok}}}
    options: {operationId: optA, responses: {"200": {description: ok}}}
    head: {operationId: headA, responses: {"200": {description: ok}}}
    patch: {operationId: patchA, responses: {"200": {description: ok}}}
    trace: {operationId: traceA}
components:
  parameters:
    PageParam: {name: page, in: query, schema: {type: integer}}
  headers:
    RateLimit: {schema: {type: integer}}
  responses:
    NotFound: {description: not found}
  callbacks:
    OnEvent:
      '{$request.body#/url}':
        post: {operationId: cbPost, responses: {"200": {description: ok}}}
`

func TestOperations_MethodsTagsServersRefs(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, opsSpec)

	// All eight HTTP methods lowered.
	methods := map[string]bool{}
	for _, g := range doc.Services[0].Groups {
		for _, op := range g.Operations {
			for _, hb := range op.Bindings.HTTP {
				methods[hb.Method] = true
			}
		}
	}
	for _, m := range []string{"GET", "PUT", "POST", "DELETE", "OPTIONS", "HEAD", "PATCH", "TRACE"} {
		assert.True(t, methods[m], "method %s lowered", m)
	}

	getA := openapitest.FindOp(t, doc, "getA")
	assert.NotEmpty(t, getA.Unmodeled, "op x-* extension")
	assert.NotEmpty(t, getA.Docs.ExternalDocs, "op externalDocs")
	_, hasServers := getA.Unmodeled["openapi:servers"]
	assert.True(t, hasServers, "path-item servers preserved")
	require.NotEmpty(t, getA.Params, "component-ref parameter resolved")
	assert.Equal(t, "page", getA.Params[0].Name.Source)

	// Component-ref response + header resolved; error ranges classified.
	require.NotEmpty(t, getA.Responses)
	assert.NotEmpty(t, getA.Responses[0].Headers, "component-ref header resolved")
	faults := map[string]bool{}
	for _, ec := range getA.Errors {
		faults[ec.Fault] = true
	}
	assert.True(t, faults["client"] && faults["server"])

	// Undeclared tag → empty tag docs (no crash), tag def registered once.
	require.Len(t, doc.TagDefs, 1)
	assert.NotEmpty(t, doc.TagDefs[0].Docs.ExternalDocs, "declared tag externalDocs")

	// Callback operation registered alongside its parent.
	assert.NotEmpty(t, openapitest.FindOp(t, doc, "cbPost").ID)
	_ = diags
}

func TestOperations_NoResponses(t *testing.T) {
	t.Parallel()
	doc, _ := parseFull(t, opsSpec)
	trace := openapitest.FindOp(t, doc, "traceA")
	assert.Empty(t, trace.Responses)
	assert.Empty(t, trace.Errors)
}

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

func TestRawChildNode(t *testing.T) {
	t.Parallel()
	assert.Nil(t, annotation.RawChildNode(nil, "x"), "nil root")
	assert.Nil(t, annotation.RawChildNode(openapitest.StrNode("x"), "k"), "non-mapping root")

	var doc yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte("a: 1\nb: 2"), &doc))
	// doc is a DocumentNode wrapping the mapping — exercises the unwrap branch.
	got := annotation.RawChildNode(&doc, "b")
	require.NotNil(t, got)
	assert.Equal(t, "2", got.Value)
	assert.Nil(t, annotation.RawChildNode(&doc, "missing"), "absent key")
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

// provenanceSpec exercises every use-site/declaration split the fix touches in
// one document: a path item shared across two mounts and mounted again as a
// webhook, a component parameter reached through a one-hop alias (PageAlias ->
// Page), a content-style component parameter, a component request body whose
// multipart encoding $refs a header, a component response with a nested
// component header, a $ref'd default response, and a component callback.
const provenanceSpec = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a: {$ref: '#/components/pathItems/Shared'}
  /b: {$ref: '#/components/pathItems/Shared'}
  /solo:
    post:
      operationId: solo
      parameters:
        - {$ref: '#/components/parameters/PageAlias'}
        - {$ref: '#/components/parameters/Filter'}
      requestBody: {$ref: '#/components/requestBodies/Body'}
      callbacks:
        onEvent: {$ref: '#/components/callbacks/Shared'}
      responses:
        "200": {$ref: '#/components/responses/OK'}
        default: {$ref: '#/components/responses/Fallback'}
webhooks:
  onShared: {$ref: '#/components/pathItems/Shared'}
components:
  pathItems:
    Shared:
      parameters:
        - {name: shared, in: query, schema: {type: string, enum: [a, b]}}
      get:
        operationId: sharedGet
        responses: {"200": {description: ok}}
  parameters:
    PageAlias: {$ref: '#/components/parameters/Page'}
    Page: {name: page, in: query, schema: {type: string, enum: [a, b]}}
    Filter:
      name: filter
      in: query
      content:
        application/json:
          schema: {type: object, properties: {q: {type: string}}}
  requestBodies:
    Body:
      content:
        multipart/form-data:
          schema: {type: object, properties: {n: {type: string}}}
          encoding:
            n:
              headers:
                X-Part: {$ref: '#/components/headers/Rate'}
  callbacks:
    Shared:
      '{$request.body#/url}':
        post:
          operationId: cbPost
          responses: {"200": {description: ok}}
  responses:
    OK:
      description: ok
      headers:
        X-Rate: {$ref: '#/components/headers/Rate'}
      content:
        application/json:
          schema: {type: object, properties: {m: {type: string}}}
    Fallback:
      description: fallback
      content:
        application/json:
          schema: {type: object, properties: {msg: {type: string}}}
  headers:
    Rate: {schema: {type: string, enum: [a, b]}}
`

// TestProvenance_EveryPointerResolvesInSource is the direct regression guard
// for issue #107's first bullet: every hoisted TypeDef, every operation, and
// every header Property must carry a Provenance.Pointer that resolves to a node
// actually present in the parsed source YAML — never a fabricated use-site
// position (e.g. under an operation that never declared the shared entity, or
// under a path whose only key is $ref) that the document has no node at.
func TestProvenance_EveryPointerResolvesInSource(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, provenanceSpec)
	openapitest.RequireNoErrorDiags(t, diags)

	var root yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte(provenanceSpec), &root))
	check := func(kind, name string, pointer jsontext.Pointer) int {
		if pointer == "" {
			return 0
		}
		assert.True(t, pointerResolves(&root, string(pointer)),
			"%s %s: pointer %q does not resolve in source", kind, name, pointer)
		return 1
	}

	checked := 0
	for id, td := range doc.Types {
		checked += check("type", string(id), td.Common().Provenance.Pointer)
	}
	require.NotEmpty(t, doc.Services, "the spec lowers to at least one service")
	for _, op := range operationsOf(doc) {
		checked += check("operation", string(op.ID), op.Provenance.Pointer)
		for _, h := range headersOf(op) {
			checked += check("header", h.WireName, h.Provenance.Pointer)
		}
	}
	assert.Positive(t, checked, "the spec must exercise at least one pointer worth checking")
}

// operationsOf flattens every operation across a document's service groups.
func operationsOf(doc *ir.Document) []ir.Operation {
	var out []ir.Operation
	for _, svc := range doc.Services {
		for _, g := range svc.Groups {
			out = append(out, g.Operations...)
		}
	}
	return out
}

// headersOf collects an operation's header Properties from both places they
// live: response headers, and the per-part headers of a multipart encoding.
func headersOf(op ir.Operation) []ir.Property {
	var out []ir.Property
	for _, resp := range op.Responses {
		out = append(out, resp.Headers...)
	}
	if op.Request == nil {
		return out
	}
	for _, c := range op.Request.Contents {
		for _, pe := range c.Encoding {
			out = append(out, pe.Headers...)
		}
	}
	return out
}

// pointerResolves reports whether an RFC 6901 JSON pointer resolves to some
// node in a parsed YAML document: each token (unescaped via jsontext.Pointer's
// own Tokens) is followed as a mapping key, or, in a sequence, a decimal index.
// The empty pointer names the whole document and resolves; callers skip it.
func pointerResolves(root *yaml.Node, pointer string) bool {
	node := root
	if node.Kind == yaml.DocumentNode && len(node.Content) > 0 {
		node = node.Content[0]
	}
	for token := range jsontext.Pointer(pointer).Tokens() {
		next, ok := pointerStep(node, token)
		if !ok {
			return false
		}
		node = next
	}
	return true
}

// pointerStep resolves one unescaped pointer segment against node: a mapping
// key match, or a sequence index for a decimal segment.
func pointerStep(node *yaml.Node, seg string) (*yaml.Node, bool) {
	switch node.Kind {
	case yaml.MappingNode:
		for i := 0; i+1 < len(node.Content); i += 2 {
			if node.Content[i].Value == seg {
				return node.Content[i+1], true
			}
		}
		return nil, false
	case yaml.SequenceNode:
		idx, err := strconv.Atoi(seg)
		if err != nil || idx < 0 || idx >= len(node.Content) {
			return nil, false
		}
		return node.Content[idx], true
	default:
		return nil, false
	}
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

const sharedDefectiveBodySpec = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a:
    post:
      operationId: postA
      requestBody: {$ref: '#/components/requestBodies/Body'}
      responses:
        "404": {$ref: '#/components/responses/Err'}
  /b:
    post:
      operationId: postB
      requestBody: {$ref: '#/components/requestBodies/Body'}
      responses:
        "404": {$ref: '#/components/responses/Err'}
components:
  requestBodies:
    Body:
      required: false
      content:
        application/json:
          schema: {type: string, required: [n]}
  responses:
    Err:
      description: err
      headers:
        X-E: {schema: {type: array, items: {type: string}}, explode: true}
      content:
        application/json:
          schema: {type: object}
`

// TestDiag_SharedDeclarationReportsEachDefectOnce pins that lowering a
// referenced component at its declaration reports each defect once. Both
// operations reach the same request body, whose scalar schema carries a
// `required` the lowered node has no field for, and the same error response,
// whose header declares an `explode` ir.Property has no field for. Reported per
// use site, a component shared by twenty operations would repeat each line
// twenty times.
//
// The second defect sits on an error response's header on purpose, so the case
// covers the shared-declaration rule on the path GitHub #422 added, not only on
// the success side.
func TestDiag_SharedDeclarationReportsEachDefectOnce(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, sharedDefectiveBodySpec)

	seen := map[string]int{}
	for _, d := range diags {
		seen[string(d.Severity)+"|"+d.Code+"|"+string(d.Provenance.Pointer)+"|"+d.Message]++
	}
	for key, n := range seen {
		assert.Equal(t, 1, n, "one defect, one diagnostic: %s", key)
	}

	// Every defect still surfaces — de-duplication must not silence any of them.
	assert.Equal(t, 2, openapitest.CountDiagsAt(diags, diag.DegradedConstruct, ir.SeverityInfo),
		"the body schema's homeless required and the error header's homeless explode are two "+
			"distinct defects")

	// The shared component's own `required: false` reaches both use sites, as
	// two values rather than one aliased pointer: lowering it at its declaration
	// de-duplicates the diagnostics, not the field.
	for _, name := range []string{"postA", "postB"} {
		op := openapitest.FindOp(t, doc, name)
		require.NotNil(t, op.Request, "%s has a body", name)
		require.NotNil(t, op.Request.Required, "%s: OpenAPI states body optionality", name)
		assert.False(t, *op.Request.Required, "%s: the component declares required: false", name)
	}
	assert.NotSame(t, openapitest.FindOp(t, doc, "postA").Request.Required,
		openapitest.FindOp(t, doc, "postB").Request.Required,
		"each use site owns its flag, so an emitter mutating one cannot reach the other")
}

// TestDiag_DistinctDefectsAtOnePointerBothSurvive is the control for the rule
// above: de-duplication compares the whole diagnostic, so two different messages
// at one pointer are two findings, not one repeated.
func TestDiag_DistinctDefectsAtOnePointerBothSurvive(t *testing.T) {
	t.Parallel()
	var c lowering.Ctx
	var acc compile.Diags
	acc.Append(c.DiagAt(ir.SeverityWarning, diag.DegradedConstruct, "/p", "first"))
	acc.Append(c.DiagAt(ir.SeverityWarning, diag.DegradedConstruct, "/p", "second"))
	acc.Append(c.DiagAt(ir.SeverityWarning, diag.DegradedConstruct, "/p", "first"))
	require.Len(t, acc.List(), 2, "the repeat is dropped, the distinct message is not")
	assert.Equal(t, "first", acc.List()[0].Message)
	assert.Equal(t, "second", acc.List()[1].Message)
}

const duplicateOperationIDSpec = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a: {$ref: '#/components/pathItems/Shared'}
  /b: {$ref: '#/components/pathItems/Shared'}
components:
  pathItems:
    Shared:
      get:
        operationId: sharedGet
        responses: {"200": {description: ok}}
`

// TestOperations_DuplicateOperationIDReported covers the collision a shared path
// item creates without the document repeating anything: one operationId written
// once, mounted twice, so two operations claim it. OpenAPI requires the id to be
// unique across the API, and nothing upstream can see this — the resolver reads
// one declaration.
func TestOperations_DuplicateOperationIDReported(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, duplicateOperationIDSpec)
	opA := opByPath(t, doc, "GET", "/a")
	opB := opByPath(t, doc, "GET", "/b")
	assert.Equal(t, opA.Name.Source, opB.Name.Source, "the IR still records what the document said")

	require.Equal(t, 1, openapitest.CountDiagsAt(diags, diag.DuplicateOperationID, ir.SeverityWarning),
		"the second claim is reported once, not both claims")
	for _, d := range diags {
		if d.Code == diag.DuplicateOperationID {
			assert.Equal(t, jsontext.Pointer("/paths/~1b/get"), d.Provenance.Pointer, "reported at the mount that collided")
			assert.Contains(t, d.Message, "/paths/~1a/get", "and names the mount that claimed it first")
		}
	}
}

// TestOperations_DistinctOperationIDsClean is the control: ordinary operations
// with their own ids, and an operation with none at all, raise nothing.
func TestOperations_DistinctOperationIDsClean(t *testing.T) {
	t.Parallel()
	_, diags := parseFull(t, openapitest.PathsSpec(`  /a:
    get:
      operationId: getA
      responses: {"200": {description: ok}}
  /b:
    get:
      responses: {"200": {description: ok}}
`))
	assert.False(t, openapitest.HasDiag(diags, diag.DuplicateOperationID))
}

// renderOperationIDDiags renders each operationId diagnostic in diags as
// "severity code pointer", sorted. Pinning the three together in one string is
// what shows a table row's severity, code and location cannot drift apart, and
// rendering every validation finding beside the compiler's two codes is what
// shows the rule table, which leaves operationId uniqueness to this package,
// adds none.
func renderOperationIDDiags(diags []ir.Diagnostic) []string {
	var out []string
	for _, d := range diags {
		if d.Code == diag.DuplicateOperationID || d.Code == diag.ConflictingOperationID ||
			strings.HasPrefix(d.Code, diag.Validation) {
			out = append(out, fmt.Sprintf("%s %s %s", d.Severity, d.Code, d.Provenance.Pointer))
		}
	}
	slices.Sort(out)
	return out
}

// TestOperations_OperationIDUniqueness is the end-to-end table over operationId
// reuse. One declaration mounted more than once, by $ref, YAML alias or merge
// key, warns at every mount but the one it is written at, else the first by
// pointer. A second declaration writing the same id is an error where it is
// written, against a path, callback, webhook or component alike.
//
// Several rows name their paths so the mount a declaration is written at sorts
// after the one reusing it; pointer order alone would warn at the declaration
// there and anchor a conflict at an alias that writes no id.
func TestOperations_OperationIDUniqueness(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		spec string
		want []string
	}{
		{
			name: "two refs mount one component declaration twice",
			spec: duplicateOperationIDSpec,
			want: []string{"warning openapi/duplicate-operation-id /paths/~1b/get"},
		},
		{
			name: "a ref to a path mounts its declaration twice (GitHub #502)",
			spec: `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a: {$ref: "#/paths/~1b"}
  /b:
    get: {operationId: dup, responses: {"200": {description: ok}}}
`,
			want: []string{"warning openapi/duplicate-operation-id /paths/~1a/get"},
		},
		{
			name: "a path-item alias mounts one declaration twice (GitHub #502)",
			spec: `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a: &item
    get:
      operationId: dup
      responses: {"200": {description: ok}}
  /b: *item
`,
			want: []string{"warning openapi/duplicate-operation-id /paths/~1b/get"},
		},
		{
			name: "a path-item alias sorting before its anchor is the mount warned",
			spec: `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /z: &item
    get:
      operationId: dup
      responses: {"200": {description: ok}}
  /a: *item
`,
			want: []string{"warning openapi/duplicate-operation-id /paths/~1a/get"},
		},
		{
			name: "two path declarations genuinely repeat the id",
			spec: openapitest.PathsSpec(`  /a:
    get: {operationId: dup, responses: {"200": {description: ok}}}
  /b:
    get: {operationId: dup, responses: {"200": {description: ok}}}
`),
			want: []string{"error openapi/conflicting-operation-id /paths/~1b/get"},
		},
		{
			name: "an operation alias mounts one declaration twice across paths",
			spec: `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a:
    get: &op
      operationId: dup
      responses: {"200": {description: ok}}
  /b:
    get: *op
`,
			want: []string{"warning openapi/duplicate-operation-id /paths/~1b/get"},
		},
		{
			name: "an operation alias sorting before its anchor is the mount warned",
			spec: `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /z:
    get: &op
      operationId: dup
      responses: {"200": {description: ok}}
  /a:
    get: *op
`,
			want: []string{"warning openapi/duplicate-operation-id /paths/~1a/get"},
		},
		{
			name: "an operation alias mounts one declaration twice across methods",
			spec: `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a:
    get: &op
      operationId: dup
      responses: {"200": {description: ok}}
    put: *op
`,
			want: []string{"warning openapi/duplicate-operation-id /paths/~1a/put"},
		},
		{
			name: "a merge key mounts one declaration twice",
			spec: `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a: &item
    get:
      operationId: dup
      responses: {"200": {description: ok}}
  /b:
    <<: *item
`,
			want: []string{"warning openapi/duplicate-operation-id /paths/~1b/get"},
		},
		{
			name: "a merge key sorting before its anchor is the mount warned",
			spec: `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /z: &item
    get:
      operationId: dup
      responses: {"200": {description: ok}}
  /a:
    <<: *item
`,
			want: []string{"warning openapi/duplicate-operation-id /paths/~1a/get"},
		},
		{
			// The pair /b writes itself overrides the one the merge key names, so
			// /b declares an operation of its own that repeats the id.
			name: "an operation overriding the one a merge key names is a second declaration",
			spec: `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a: &item
    get:
      operationId: dup
      responses: {"200": {description: ok}}
  /b:
    <<: *item
    get:
      operationId: dup
      responses: {"200": {description: ok}}
`,
			want: []string{"error openapi/conflicting-operation-id /paths/~1b/get"},
		},
		{
			name: "a two-deep ref chain mounts one declaration twice",
			spec: `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a: {$ref: '#/components/pathItems/Shared'}
  /b: {$ref: '#/paths/~1a'}
components:
  pathItems:
    Shared:
      get: {operationId: dup, responses: {"200": {description: ok}}}
`,
			want: []string{"warning openapi/duplicate-operation-id /paths/~1b/get"},
		},
		{
			name: "a callback operation genuinely repeats a path operation's id",
			spec: `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a:
    post:
      operationId: dup
      responses: {"200": {description: ok}}
  /b:
    post:
      operationId: parent
      callbacks:
        onEvent:
          '{$request.body#/url}':
            post: {operationId: dup, responses: {"200": {description: ok}}}
      responses: {"200": {description: ok}}
`,
			want: []string{
				"error openapi/conflicting-operation-id /paths/~1b/post/callbacks/onEvent/{$request.body#~1url}/post",
			},
		},
		{
			name: "a webhook genuinely repeats a path operation's id",
			spec: `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a:
    post: {operationId: dup, responses: {"200": {description: ok}}}
webhooks:
  hook:
    post: {operationId: dup, responses: {"200": {description: ok}}}
`,
			want: []string{"error openapi/conflicting-operation-id /webhooks/hook/post"},
		},
		{
			name: "a webhook reusing a path item by ref mounts its declaration twice",
			spec: `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a:
    post: {operationId: dup, responses: {"200": {description: ok}}}
webhooks:
  hook: {$ref: '#/paths/~1a'}
`,
			want: []string{"warning openapi/duplicate-operation-id /webhooks/hook/post"},
		},
		{
			name: "two webhooks genuinely repeat one id",
			spec: `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
webhooks:
  h1:
    post: {operationId: dup, responses: {"200": {description: ok}}}
  h2:
    post: {operationId: dup, responses: {"200": {description: ok}}}
`,
			want: []string{"error openapi/conflicting-operation-id /webhooks/h2/post"},
		},
		{
			name: "an alias between two components mounts one declaration twice",
			spec: `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a: {$ref: '#/components/pathItems/One'}
  /b: {$ref: '#/components/pathItems/Two'}
components:
  pathItems:
    One: &shared
      get: {operationId: dup, responses: {"200": {description: ok}}}
    Two: *shared
`,
			want: []string{"warning openapi/duplicate-operation-id /paths/~1b/get"},
		},
		{
			name: "two components each ref'd once genuinely repeat the id",
			spec: `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a: {$ref: '#/components/pathItems/One'}
  /b: {$ref: '#/components/pathItems/Two'}
components:
  pathItems:
    One:
      get: {operationId: dup, responses: {"200": {description: ok}}}
    Two:
      get: {operationId: dup, responses: {"200": {description: ok}}}
`,
			want: []string{"error openapi/conflicting-operation-id /components/pathItems/Two/get"},
		},
		{
			name: "an alias remount and a genuine repeat both name one id",
			spec: `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a: &item
    get:
      operationId: dup
      responses: {"200": {description: ok}}
  /b: *item
  /c:
    get: {operationId: dup, responses: {"200": {description: ok}}}
`,
			want: []string{
				"error openapi/conflicting-operation-id /paths/~1c/get",
				"warning openapi/duplicate-operation-id /paths/~1b/get",
			},
		},
		{
			// /z writes the second declaration and /b only aliases it, so the
			// conflict belongs at /z even though /b sorts first of the two.
			name: "a repeat is reported where it is written, not at an alias of it",
			spec: `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a:
    get: {operationId: dup, responses: {"200": {description: ok}}}
  /z: &item
    get:
      operationId: dup
      responses: {"200": {description: ok}}
  /b: *item
`,
			want: []string{
				"error openapi/conflicting-operation-id /paths/~1z/get",
				"warning openapi/duplicate-operation-id /paths/~1b/get",
			},
		},
		{
			// Both parents $ref one callback component, so its operation is one
			// declaration the two of them mount, not a declaration of each.
			name: "a callback ref'd from two parent operations mounts one declaration twice",
			spec: sharedCallbackSpec,
			want: []string{
				"warning openapi/duplicate-operation-id /paths/~1d/post/callbacks/onEvent/{$request.body#~1url}/post",
			},
		},
		{
			// /b declares get and put sharing one id, and /a mounts both again by
			// referencing /b whole. get and put are two declarations, so besides
			// the remount they conflict with each other: an error where put writes
			// the id, and a warning at each of /a's mounts, where the reuse is.
			name: "a ref'd item declaring get and put that share an id",
			spec: `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /b:
    get: {operationId: dup, responses: {"200": {description: ok}}}
    put: {operationId: dup, responses: {"200": {description: ok}}}
  /a: {$ref: '#/paths/~1b'}
`,
			want: []string{
				"error openapi/conflicting-operation-id /paths/~1b/put",
				"warning openapi/duplicate-operation-id /paths/~1a/get",
				"warning openapi/duplicate-operation-id /paths/~1a/put",
			},
		},
		{
			name: "an additionalOperations entry genuinely repeats a method's id",
			spec: `openapi: 3.2.0
info: {title: T, version: "1"}
paths:
  /a:
    get: {operationId: dup, responses: {"200": {description: ok}}}
    additionalOperations:
      COPY: {operationId: dup, responses: {"200": {description: ok}}}
`,
			want: []string{"error openapi/conflicting-operation-id /paths/~1a/get"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, diags := parseFull(t, tc.spec)
			assert.Empty(t, cmp.Diff(tc.want, renderOperationIDDiags(diags)))
		})
	}
}

// TestOperations_SelfReferencesByFileName pins the reuse a reference naming
// this document by its file name makes. The compiler reads it as internal, so
// its claim has the plain reference's pointer.
//
// The last case is a self-reference the compiler does not read as one, since
// its document part carries a directory (GitHub #576), so its pointer is
// another. The resolver answers it from the source as held (GitHub #759), so
// it shares the declaring node, and that is what makes it a reuse.
//
// A reader is needed because the resolver counts a document part as leaving
// the document.
func TestOperations_SelfReferencesByFileName(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		paths string
		want  []string
	}{
		{
			name: "two spellings of one component",
			paths: `  /a: {$ref: '#/components/pathItems/Shared'}
  /b: {$ref: 'self.yaml#/components/pathItems/Shared'}
`,
			want: []string{"warning openapi/duplicate-operation-id /paths/~1b/get"},
		},
		{
			name: "a path and a self-reference to it",
			paths: `  /a:
    get: {operationId: dup, responses: {"200": {description: ok}}}
  /b: {$ref: 'self.yaml#/paths/~1a'}
`,
			want: []string{"warning openapi/duplicate-operation-id /paths/~1b/get"},
		},
		{
			name: "a self-reference through a directory, read as another document's pointer (GitHub #576)",
			paths: `  /a: {$ref: '#/components/pathItems/Shared'}
  /b: {$ref: './self.yaml#/components/pathItems/Shared'}
`,
			want: []string{"warning openapi/duplicate-operation-id /paths/~1b/get"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			spec := `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
` + tc.paths + `components:
  pathItems:
    Shared:
      get: {operationId: dup, responses: {"200": {description: ok}}}
`
			path := filepath.Join(t.TempDir(), "self.yaml")
			require.NoError(t, os.WriteFile(path, []byte(spec), 0o600))

			doc, diags, err := openapi.New().Compile(t.Context(),
				[]compilers.Source{{Path: path, Data: []byte(spec)}},
				compilers.Options{Reader: reader.Disk{}, FormatOptions: openapi.Options{AllowExternalRefs: true}})

			require.NoError(t, err)
			require.NotNil(t, doc, "compile refused: %+v", diags)
			assert.Empty(t, cmp.Diff(tc.want, renderOperationIDDiags(diags)))
		})
	}
}

// TestOperations_OperationIDFindingsIgnoreDeclarationOrder is the two-order
// half of the table above: harness.Check stops at the first error diagnostic,
// so its order-invariance oracle never reaches conflicting-operation-id, and a
// hand-written case is the only thing that can pin it. Each pair declares the
// same document with two of its entries swapped, the first in an order the
// check this replaced got wrong: it reported whichever claim was lowered
// second, so a document and its reverse named different operations.
func TestOperations_OperationIDFindingsIgnoreDeclarationOrder(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name        string
		first, then string
	}{
		{
			name: "two genuinely repeated path operations",
			first: openapitest.PathsSpec(`  /b:
    get: {operationId: dup, responses: {"200": {description: ok}}}
  /a:
    get: {operationId: dup, responses: {"200": {description: ok}}}
`),
			then: openapitest.PathsSpec(`  /a:
    get: {operationId: dup, responses: {"200": {description: ok}}}
  /b:
    get: {operationId: dup, responses: {"200": {description: ok}}}
`),
		},
		{
			name: "two webhooks genuinely repeating one id",
			first: `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
webhooks:
  h2:
    post: {operationId: dup, responses: {"200": {description: ok}}}
  h1:
    post: {operationId: dup, responses: {"200": {description: ok}}}
`,
			then: `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
webhooks:
  h1:
    post: {operationId: dup, responses: {"200": {description: ok}}}
  h2:
    post: {operationId: dup, responses: {"200": {description: ok}}}
`,
		},
		{
			name: "a ref remounting one component at two paths",
			first: `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /b: {$ref: '#/components/pathItems/Shared'}
  /a: {$ref: '#/components/pathItems/Shared'}
components:
  pathItems:
    Shared:
      get: {operationId: dup, responses: {"200": {description: ok}}}
`,
			then: `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a: {$ref: '#/components/pathItems/Shared'}
  /b: {$ref: '#/components/pathItems/Shared'}
components:
  pathItems:
    Shared:
      get: {operationId: dup, responses: {"200": {description: ok}}}
`,
		},
		{
			name: "a ref remounting the path it names",
			first: `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a: {$ref: '#/paths/~1b'}
  /b:
    get: {operationId: dup, responses: {"200": {description: ok}}}
`,
			then: `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /b:
    get: {operationId: dup, responses: {"200": {description: ok}}}
  /a: {$ref: '#/paths/~1b'}
`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, firstDiags := parseFull(t, tc.first)
			_, thenDiags := parseFull(t, tc.then)
			first := renderOperationIDDiags(firstDiags)
			require.NotEmpty(t, first, "the case reports something to compare")
			assert.Empty(t, cmp.Diff(first, renderOperationIDDiags(thenDiags)))
		})
	}
}

// TestOperation_UnserializableExtensionStillWarns pins the operation's half of
// what TestAuth_UnserializableExtensionStillWarns pins for a security scheme: an
// x-* extension that cannot be serialized is announced even though nothing of it
// is kept, so the operation's own Unmodeled ends up empty.
//
// The warning and the entry travel separately, and only the entry is gated on
// being non-empty. Recording one without the other is the shape this catches —
// an operation that quietly loses an extension it could not represent.
func TestOperation_UnserializableExtensionStillWarns(t *testing.T) {
	t.Parallel()
	spec := openapitest.PathsSpec(`  /x:
    get:
      x-bad: {1: intkey}
      responses: {"200": {description: ok}}
`)
	doc, diags := parseFull(t, spec)

	var announced bool
	for _, d := range diags {
		if d.Code == diag.DegradedConstruct && d.Severity == ir.SeverityWarning &&
			strings.Contains(d.Message, "x-bad") {
			announced = true
		}
	}
	assert.True(t, announced,
		"the unserializable extension is named in a warning, not merely some warning: %+v", diags)
	op := doc.Services[0].Groups[0].Operations[0]
	assert.Empty(t, op.Unmodeled, "and is dropped rather than stored half-converted")
}

// ghostRefsSpec references non-existent components everywhere so every
// resolve-or-skip path (unresolved GetObject → nil) and resolution-error branch
// is exercised without a panic.
const ghostRefsSpec = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a:
    parameters:
      - {$ref: '#/components/parameters/GhostParam'}
    get:
      operationId: getA
      callbacks:
        good:
          '{$url}': {$ref: '#/components/pathItems/GhostInner'}
        bad: {$ref: '#/components/callbacks/GhostCb'}
      requestBody: {$ref: '#/components/requestBodies/GhostBody'}
      responses:
        "200": {$ref: '#/components/responses/GhostResp'}
        "201":
          description: ok
          headers:
            X-H: {$ref: '#/components/headers/GhostHeader'}
          content:
            application/json:
              schema: {type: string}
              examples:
                one: {$ref: '#/components/examples/GhostEx'}
  /ref: {$ref: '#/components/pathItems/GhostItem'}
webhooks:
  hook: {$ref: '#/components/pathItems/GhostHook'}
`

// TestGhostRefs_AllResolversDegradeGracefully drives every reference-or-inline
// entry the operation walk resolves against a component that does not exist, so
// each resolve-or-skip branch is reached in one pass.
//
// It holds what the skips do, not only that nothing panics. Each of them decides
// whether a construct the document does not actually declare ends up in the IR.
func TestGhostRefs_AllResolversDegradeGracefully(t *testing.T) {
	t.Parallel()
	// A reference that resolves to nothing is a diagnostic, not a parse failure,
	// so the whole spec compiles and every skip is reached in one pass.
	doc, diags := parseFull(t, ghostRefsSpec)
	require.NotEmpty(t, diags, "unresolved refs reported")

	// And the skips themselves are silent: every diagnostic here is the load
	// phase's report of a $ref it followed to nothing, at the $ref itself
	// (GitHub #385). A skip lowering an empty stand-in instead would report on a
	// construct the document never wrote, beside the one report per $ref below.
	wantRefs := map[string]string{
		"/paths/~1a/parameters/0":                                             "#/components/parameters/GhostParam",
		"/paths/~1a/get/callbacks/good/{$url}":                                "#/components/pathItems/GhostInner",
		"/paths/~1a/get/callbacks/bad":                                        "#/components/callbacks/GhostCb",
		"/paths/~1a/get/requestBody":                                          "#/components/requestBodies/GhostBody",
		"/paths/~1a/get/responses/200":                                        "#/components/responses/GhostResp",
		"/paths/~1a/get/responses/201/headers/X-H":                            "#/components/headers/GhostHeader",
		"/paths/~1a/get/responses/201/content/application~1json/examples/one": "#/components/examples/GhostEx",
		"/paths/~1ref":                                                        "#/components/pathItems/GhostItem",
		"/webhooks/hook":                                                      "#/components/pathItems/GhostHook",
	}
	require.Len(t, diags, len(wantRefs),
		"one report per ghost reference, not a second added by a lowering resolver: %+v", diags)
	for pointer, ref := range wantRefs {
		msg := openapitest.DiagMessageAt(t, diags, diag.UnresolvedRef, ir.SeverityError, pointer)
		assert.Contains(t, msg, fmt.Sprintf("unresolved $ref %q", ref), "names the reference that failed")
	}

	// What each skip has to do is contribute nothing — not an empty stand-in.
	// Asserting only "no panic" leaves that unheld: lowering an unresolvable path
	// item as an empty one, rather than skipping it, passes a no-panic test and
	// every other test in this repo, while putting a path the document does not
	// declare into the IR.
	require.Len(t, doc.Services[0].Groups, 1, "the ghost webhook adds no group of its own")
	ops := doc.Services[0].Groups[0].Operations
	require.Len(t, ops, 1,
		"only the one declared operation survives: the ghost path item, the ghost webhook, the "+
			"ghost callback and the callback's ghost path item each contribute none")
	op := ops[0]
	assert.Equal(t, "getA", op.Name.Source)
	assert.Empty(t, op.Params, "the ghost parameter is skipped, not lowered as a nameless one")
	assert.Nil(t, op.Request, "as is the ghost request body")

	require.Len(t, op.Responses, 1, "the ghost response is skipped; the inline 201 is not")
	resp := op.Responses[0]
	assert.Empty(t, resp.Headers, "the ghost header is skipped")
	require.NotNil(t, resp.Payload)
	require.Len(t, resp.Payload.Contents, 1)
	assert.Empty(t, resp.Payload.Contents[0].Examples, "and the ghost example")
}

// TestOperation_DeprecatedIsCarried pins the one flag an operation can raise
// about itself. It is a declared fact, not an inference, so it lands on the
// operation rather than in Unmodeled — and an SDK that hides deprecated calls
// has nothing else to read.
func TestOperation_DeprecatedIsCarried(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.PathsSpec(`  /a:
    get: {operationId: getA, deprecated: true, responses: {"200": {description: ok}}}
    post: {operationId: postA, responses: {"200": {description: ok}}}
`))
	openapitest.RequireNoErrorDiags(t, diags)

	assert.NotNil(t, openapitest.FindOp(t, doc, "getA").Deprecation, "the declared flag is carried")
	assert.Nil(t, openapitest.FindOp(t, doc, "postA").Deprecation, "and an operation that declares none has none")
}

// operationServersSpec declares `servers` at both levels OpenAPI allows, on an
// operation of each of the three routes that lower one, with a distinct URL
// everywhere so a preserved list can be traced to the object that wrote it. The
// last two path items cover the single-level shapes.
const operationServersSpec = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /both:
    servers: [{url: 'https://pathitem.example'}]
    post:
      operationId: postBoth
      servers: [{url: 'https://operation.example'}]
      callbacks:
        onEvent:
          '{$request.body#/url}':
            servers: [{url: 'https://cb-pathitem.example'}]
            post:
              operationId: onEvent
              servers: [{url: 'https://cb-operation.example'}]
              responses: {"200": {description: ok}}
      responses: {"200": {description: ok}}
  /operationOnly:
    get:
      operationId: getOperationOnly
      servers: [{url: 'https://only.example'}]
      responses: {"200": {description: ok}}
  /pathItemOnly:
    servers: [{url: 'https://pathonly.example'}]
    get:
      operationId: getPathItemOnly
      responses: {"200": {description: ok}}
webhooks:
  hooked:
    servers: [{url: 'https://wh-pathitem.example'}]
    post:
      operationId: onHook
      servers: [{url: 'https://wh-operation.example'}]
      responses: {"200": {description: ok}}
`

// TestOperations_OwnServersKeptBesideThePathItems pins the overriding half of
// the servers pair, on every route that lowers an operation. An Operation
// Object's servers override the Path Item Object's, so both lists must be kept:
// keeping only the path item's would send an emitter to a host the operation
// had replaced, with nothing reporting it.
//
// All three routes are asserted because a single-route case cannot see a fix
// that skips the other two (GitHub #39). The two levels sit under separate keys
// because they are two declarations at two pointers; one key would make the
// surviving list depend on which lowering ran last.
func TestOperations_OwnServersKeptBesideThePathItems(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, operationServersSpec)
	openapitest.RequireNoErrorDiags(t, diags)

	// own/inherited are the operation's own list and its path item's; reported is
	// where the operation's degradation is stamped (the operation itself).
	for _, tc := range []struct{ op, own, ownAt, inherited, inheritedAt, reported string }{
		{"postBoth",
			"https://operation.example", "/paths/~1both/post/servers",
			"https://pathitem.example", "/paths/~1both/servers", "/paths/~1both/post"},
		{"onHook",
			"https://wh-operation.example", "/webhooks/hooked/post/servers",
			"https://wh-pathitem.example", "/webhooks/hooked/servers", "/webhooks/hooked/post"},
		{"onEvent",
			"https://cb-operation.example",
			"/paths/~1both/post/callbacks/onEvent/{$request.body#~1url}/post/servers",
			"https://cb-pathitem.example",
			"/paths/~1both/post/callbacks/onEvent/{$request.body#~1url}/servers",
			"/paths/~1both/post/callbacks/onEvent/{$request.body#~1url}/post"},
	} {
		op := openapitest.FindOp(t, doc, tc.op)

		own, ok := op.Unmodeled["openapi:operationServers"]
		require.True(t, ok, "%s keeps its operation's own servers", tc.op)
		assert.Equal(t, ir.ReasonNoIRHome, own.Reason)
		assert.JSONEq(t, `[{"url":"`+tc.own+`"}]`, string(own.Value),
			"%s keeps its own list, not its path item's", tc.op)
		assert.Equal(t, jsontext.Pointer(tc.ownAt), own.Provenance.Pointer)

		inherited, ok := op.Unmodeled["openapi:servers"]
		require.True(t, ok, "%s keeps its path item's list beside it, not replaced by it", tc.op)
		assert.JSONEq(t, `[{"url":"`+tc.inherited+`"}]`, string(inherited.Value))
		assert.Equal(t, jsontext.Pointer(tc.inheritedAt), inherited.Provenance.Pointer,
			"each entry keeps the coordinate of the object that declared it")

		assert.True(t, openapitest.HasDiagCodeAt(diags, diag.DegradedConstruct, tc.reported),
			"%s reports the operation's own list as the path item's already was", tc.op)
	}
}

// TestOperations_ServersKeysAreIndependent is the control for the test above:
// each key records only what its own object declared, so an operation declaring
// one level records that level alone rather than both.
func TestOperations_ServersKeysAreIndependent(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, operationServersSpec)
	openapitest.RequireNoErrorDiags(t, diags)

	opOnly := openapitest.FindOp(t, doc, "getOperationOnly")
	assert.Contains(t, opOnly.Unmodeled, "openapi:operationServers")
	assert.NotContains(t, opOnly.Unmodeled, "openapi:servers",
		"an operation whose path item declares none records none for it")

	pathOnly := openapitest.FindOp(t, doc, "getPathItemOnly")
	assert.Contains(t, pathOnly.Unmodeled, "openapi:servers")
	assert.NotContains(t, pathOnly.Unmodeled, "openapi:operationServers",
		"and an operation declaring none of its own records none")
}

// TestOperations_OwnServersSurviveBesideExtensions pins an ordering constraint
// inside lowerOperation that nothing else reaches. The operation's extensions
// are assigned to op.Unmodeled wholesale, so preserving the servers before that
// assignment discards them — a map replacement, not a merge. No other fixture
// declares both on one operation, so without this the constraint is a comment
// that a later edit can silently break.
func TestOperations_OwnServersSurviveBesideExtensions(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.PathsSpec(`  /x:
    get:
      operationId: getX
      servers: [{url: 'https://operation.example'}]
      x-vendor: kept
      responses: {"200": {description: ok}}
`))
	openapitest.RequireNoErrorDiags(t, diags)

	op := openapitest.FindOp(t, doc, "getX")
	servers, ok := op.Unmodeled["openapi:operationServers"]
	require.True(t, ok, "the servers survive the extensions assignment")
	assert.JSONEq(t, `[{"url":"https://operation.example"}]`, string(servers.Value))
	assert.Contains(t, op.Unmodeled, "openapi:x-vendor",
		"and the extensions survive beside them, so neither overwrote the other")
}

// TestRefSiteDocs_MountsKeepTheirOwnInEitherOrder is the hand-rolled two-order
// diff for GitHub #610. A use-site summary or description is neither part of the
// type registry nor a diagnostic, so the in-package order-invariance oracle does
// not reach it. One component referenced by two operations with different
// overrides, declared in both orders, must give each mount its own docs.
func TestRefSiteDocs_MountsKeepTheirOwnInEitherOrder(t *testing.T) {
	t.Parallel()
	const first = `  /a:
    get:
      operationId: a
      parameters: [{$ref: '#/components/parameters/Limit', description: from a}]
      responses: {"200": {description: ok}}
`
	const second = `  /b:
    get:
      operationId: b
      parameters: [{$ref: '#/components/parameters/Limit', description: from b}]
      responses: {"200": {description: ok}}
`
	projection := func(paths string) map[string]string {
		t.Helper()
		spec := `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
` + paths + `components:
  parameters:
    Limit: {name: limit, in: query, description: declared, schema: {type: integer}}
`
		doc, _, diags := lowerServiceSpec(t, spec)
		openapitest.RequireNoErrorDiags(t, diags)
		out := map[string]string{}
		for _, name := range []string{"a", "b"} {
			p, ok := paramBySource(openapitest.FindOp(t, doc, name), "limit")
			require.True(t, ok)
			out[name] = p.Docs.Description
		}
		return out
	}
	assert.Empty(t, cmp.Diff(projection(first+second), projection(second+first)),
		"each mount keeps its own override whichever was lowered first")
}

// TestXML_NodeTypeIsReadAtEveryVersion pins the 3.2 XML nodeType:
// ir.XMLHints.NodeType already existed and its GoDoc already named the version,
// so this is the wiring. Below 3.2 it reads the same, and load adds that the
// field is a later version's (GitHub #864).
func TestXML_NodeTypeIsReadAtEveryVersion(t *testing.T) {
	t.Parallel()
	const body = `    S:
      type: object
      properties:
        n:
          type: string
          xml: {nodeType: text}
`
	for _, version := range []string{"3.2.0", "3.1.0", "3.0.3"} {
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			doc, _, diags := lowerServiceSpec(t, "openapi: "+version+
				"\ninfo: {title: T, version: \"1\"}\npaths: {}\ncomponents:\n  schemas:\n"+body)
			openapitest.RequireNoErrorDiags(t, diags)
			model, ok := doc.Types[ir.TypeID("t/openapi/components/schemas/S")].(*ir.Model)
			require.True(t, ok)
			prop, ok := openapitest.PropsByWire(model.Properties)["n"]
			require.True(t, ok)
			require.NotNil(t, prop.XML)
			assert.Equal(t, "text", prop.XML.NodeType)
			assert.False(t, openapitest.HasDiag(diags, diag.UnknownObjectKey))
			assert.Equal(t, version != "3.2.0", openapitest.HasDiagCodeAt(diags, diag.LaterVersionConstruct,
				"/components/schemas/S/properties/n/xml/nodeType"))
		})
	}
}

// TestContent_UnkeepableMediaTypeRefIsReported pins the media type $ref that
// cannot be kept verbatim, a mapping writing one key twice: it is reported as
// lost, and nothing claims it was kept.
func TestContent_UnkeepableMediaTypeRefIsReported(t *testing.T) {
	t.Parallel()
	_, _, diags := lowerServiceSpec(t, openapitest.PathsSpecVer("3.2.0", `  /a:
    get:
      operationId: a
      responses:
        "200":
          description: ok
          content:
            application/json: {$ref: {a: 1, a: 2}}
`))
	openapitest.AssertHasCode(t, diags, diag.UnpreservableConstruct, ir.SeverityError)
	assert.False(t, openapitest.HasDiagCodeAt(diags, diag.DegradedConstruct,
		"/paths/~1a/get/responses/200/content/application~1json/$ref"), "nothing announces a keep it did not make")
}

// TestContent_MediaTypeRefIsKeptNotFollowed pins a content entry written as a
// Reference Object while this compiler does not follow one (GitHub #856): the
// $ref and the summary beside it are kept verbatim and one warning says the
// entry lowered as written. Below 3.2, which does not allow it, it lowers the
// same, and load adds that the construct is a later version's (GitHub #864).
func TestContent_MediaTypeRefIsKeptNotFollowed(t *testing.T) {
	t.Parallel()
	const at = "/paths/~1a/get/responses/200/content/application~1json/$ref"
	const paramAt = "/paths/~1a/get/parameters/0/content/application~1json/$ref"
	for _, version := range []string{"3.2.0", "3.1.0"} {
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			_, svc, diags := lowerServiceSpec(t, openapitest.PathsSpecVer(version, `  /a:
    get:
      operationId: a
      parameters:
        - name: f
          in: query
          content:
            application/json: {$ref: '#/components/mediaTypes/Json'}
      responses:
        "200":
          description: ok
          content:
            application/json: {$ref: '#/components/mediaTypes/Json', summary: S}
`))
			op := openapitest.FirstOp(t, svc)
			content := op.Responses[0].Payload.Contents[0]
			assert.Equal(t, ir.ReasonDegradedLowering, content.Unmodeled["openapi:$ref"].Reason)
			assert.Contains(t, content.Unmodeled, "openapi:summary", "nothing beside the $ref is dropped")
			assert.True(t, openapitest.HasDiagCodeAt(diags, diag.DegradedConstruct, at), "got %v", diags)
			require.Len(t, op.Params, 1)
			assert.Equal(t, ir.ReasonDegradedLowering, op.Params[0].Unmodeled["openapi:content/application~1json/$ref"].Reason)
			assert.True(t, openapitest.HasDiagCodeAt(diags, diag.DegradedConstruct, paramAt))
			assert.False(t, openapitest.HasDiag(diags, diag.UnknownObjectKey))
			later := version != "3.2.0"
			assert.Equal(t, later, openapitest.HasDiagCodeAt(diags, diag.LaterVersionConstruct, at))
			assert.Equal(t, later, openapitest.HasDiagCodeAt(diags, diag.LaterVersionConstruct, paramAt))
		})
	}
}

// TestContent_MediaTypeDescriptionIsKept pins the description OpenAPI 3.2 gives
// a Media Type Object, which the bundled model names no field for and
// ir.Content has no docs to hold: at a body's content and at a parameter's, it
// is kept verbatim with an info rather than called undefined. Below 3.2 it is
// kept the same, and load adds that the field is a later version's.
func TestContent_MediaTypeDescriptionIsKept(t *testing.T) {
	t.Parallel()
	const at = "/paths/~1a/get/responses/200/content/application~1json/description"
	const paramAt = "/paths/~1a/get/parameters/0/content/application~1json/description"
	for _, version := range []string{"3.2.0", "3.1.0"} {
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			_, svc, diags := lowerServiceSpec(t, openapitest.PathsSpecVer(version, `  /a:
    get:
      operationId: a
      parameters:
        - name: f
          in: query
          content:
            application/json: {description: A filter., schema: {type: string}}
      responses:
        "200":
          description: ok
          content:
            application/json: {description: The body., schema: {type: string}}
`))
			openapitest.RequireNoErrorDiags(t, diags)
			op := openapitest.FirstOp(t, svc)
			kept := op.Responses[0].Payload.Contents[0].Unmodeled["openapi:description"]
			assert.Equal(t, ir.ReasonNoIRHome, kept.Reason)
			assert.JSONEq(t, `"The body."`, string(kept.Value))
			require.Len(t, op.Params, 1)
			assert.Equal(t, ir.ReasonNoIRHome,
				op.Params[0].Unmodeled["openapi:content/application~1json/description"].Reason)
			openapitest.AssertInfoDiagAt(t, diags, at)
			openapitest.AssertInfoDiagAt(t, diags, paramAt)
			assert.False(t, openapitest.HasDiag(diags, diag.UnknownObjectKey))
			assert.Equal(t, version != "3.2.0", openapitest.HasDiagCodeAt(diags, diag.LaterVersionConstruct, at))
		})
	}
}
