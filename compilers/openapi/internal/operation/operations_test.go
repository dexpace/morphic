package operation_test

import (
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/ir"
)

func TestGrouping_ByFirstTag(t *testing.T) {
	t.Parallel()
	spec := `openapi: 3.1.0
info: {title: T, version: "1"}
tags:
  - {name: users, description: User ops}
paths:
  /users:
    get:
      operationId: listUsers
      tags: [users, admin]
      responses: {"200": {description: ok}}
  /misc:
    get:
      operationId: misc
      responses: {"200": {description: ok}}
`
	doc, svc, diags := lowerServiceSpec(t, spec)
	requireNoErrorDiags(t, diags)
	require.Len(t, svc.Groups, 2)
	byName := indexBy(svc.Groups, func(g ir.OperationGroup) string { return g.Name.Source })
	users, ok := byName["users"]
	require.True(t, ok)
	assert.Equal(t, "User ops", users.Docs.Description)
	require.Len(t, users.Operations, 1)
	op := users.Operations[0]
	assert.Equal(t, ir.OpID("op/openapi/paths/~1users/get"), op.ID)
	assert.Equal(t, []string{"users", "admin"}, op.Tags)
	def, ok := byName[""]
	require.True(t, ok, "untagged op lands in the default group")
	assert.Equal(t, "default", def.Name.Hint)
	require.Len(t, doc.TagDefs, 1) // declared tag metadata registered once
}

func TestResponses_ErrorSplitAndRanges(t *testing.T) {
	t.Parallel()
	spec := pathsSpec(`  /w:
    get:
      operationId: w
      responses:
        "200": {description: ok}
        "404":
          description: missing
          content: {application/json: {schema: {type: object, properties: {msg: {type: string}}}}}
        "5XX": {description: server oops}
        default: {description: anything else}
`)
	_, svc, diags := lowerServiceSpec(t, spec)
	requireNoErrorDiags(t, diags)
	op := firstOp(t, svc)
	require.Len(t, op.Responses, 1)
	assert.Equal(t, []ir.StatusRange{{From: 200, To: 200}}, op.Responses[0].Conditions.StatusCodes)
	require.Len(t, op.Errors, 3)
	assert.Equal(t, []ir.StatusRange{{From: 404, To: 404}}, op.Errors[0].Conditions.StatusCodes)
	assert.Equal(t, "client", op.Errors[0].Fault)
	assert.NotEmpty(t, op.Errors[0].Type.Target, "404 error model lowered and referenced")
	assert.Equal(t, []ir.StatusRange{{From: 500, To: 599}}, op.Errors[1].Conditions.StatusCodes)
	assert.Equal(t, "server", op.Errors[1].Fault)
	assert.Equal(t, []ir.StatusRange{{From: 0, To: 0}}, op.Errors[2].Conditions.StatusCodes)
	assert.Equal(t, "", op.Errors[2].Fault)
}

// TestResponses_NamedByStatusKey pins the only naming OpenAPI gives a response.
// It declares none — it keys responses by status code — so Response.Name carries
// the hint ir-design §7.2 calls for there, derived from the key the response is
// filed under. Every response used to reach the IR with all three name channels
// empty, leaving an emitter naming a per-response result type nothing to build
// one from (GitHub #259).
//
// The error half of the same map has no counterpart to check: ir.ErrorCase
// carries no Naming at all, so there is no channel on it to leave empty.
func TestResponses_NamedByStatusKey(t *testing.T) {
	t.Parallel()
	spec := pathsSpec(`  /w:
    get:
      operationId: w
      responses:
        "200": {description: ok}
        "2XX": {description: any success}
        "": {description: no status code at all}
        "404": {description: missing}
        default: {description: anything else}
`)
	_, svc, diags := lowerServiceSpec(t, spec)
	requireNoErrorDiags(t, diags)
	op := firstOp(t, svc)

	require.Len(t, op.Responses, 3)
	hints := make([]string, 0, len(op.Responses))
	for _, r := range op.Responses {
		assert.Empty(t, r.Name.Source, "OpenAPI declares no response name, so Source stays empty")
		hints = append(hints, r.Name.Hint)
	}
	assert.Equal(t, []string{"200", "2_xx", "empty"}, hints,
		"the key as declared, neutralized; a key with no word in it takes the mint")
}

// TestResponses_InvalidStatusKeyIsReported is the whole of GitHub #262 at the
// lowering level: a key naming no status used to be folded into {0,0} — the
// range "default" denotes — with no diagnostic, so a typo and a declared default
// arrived as the same shape. Both are declared here, and they must not.
func TestResponses_InvalidStatusKeyIsReported(t *testing.T) {
	t.Parallel()
	spec := pathsSpec(`  /w:
    get:
      operationId: w
      responses:
        "200": {description: ok}
        "wat":
          description: names no status
          content: {application/json: {schema: {type: object, properties: {a: {type: string}}}}}
        default: {description: anything else}
`)
	_, svc, diags := lowerServiceSpec(t, spec)
	requireNoErrorDiags(t, diags)
	op := firstOp(t, svc)

	msg := diagMessageAt(t, diags, diag.InvalidStatusKey, ir.SeverityWarning,
		"/paths/~1w/get/responses/wat")
	assert.Contains(t, msg, `"wat"`, "the message names the key that could not be read")

	require.Len(t, op.Responses, 2)
	byHint := indexBy(op.Responses, func(r ir.Response) string { return r.Name.Hint })
	ok200, found := byHint["200"]
	require.True(t, found)
	assert.Equal(t, []ir.StatusRange{{From: 200, To: 200}}, ok200.Conditions.StatusCodes)

	unreadable, found := byHint["wat"]
	require.True(t, found, "the response is kept, and its key survives as the name hint")
	assert.Empty(t, unreadable.Conditions.StatusCodes,
		"no status could be read, so none is claimed — {0,0} would be default's own range")
	require.NotNil(t, unreadable.Payload, "the body is real and is not dropped over a bad key")
	assert.NotEmpty(t, unreadable.Payload.Contents)

	require.Len(t, op.Errors, 1, "the declared default is still the only catch-all")
	assert.Equal(t, []ir.StatusRange{{From: 0, To: 0}}, op.Errors[0].Conditions.StatusCodes)
}

// TestResponses_ValidStatusKeysAreNotReported is the overreach guard: a check
// that rejected anything unusual would satisfy the test above while warning on
// documents that are entirely correct.
func TestResponses_ValidStatusKeysAreNotReported(t *testing.T) {
	t.Parallel()
	spec := pathsSpec(`  /w:
    get:
      operationId: w
      responses:
        "100": {description: continue}
        "200": {description: ok}
        "2XX": {description: any success}
        "599": {description: the top of what HTTP defines}
        "4xx": {description: lowercase wildcard}
        default: {description: anything else}
`)
	_, _, diags := lowerServiceSpec(t, spec)
	requireNoErrorDiags(t, diags)
	assert.False(t, hasDiag(diags, diag.InvalidStatusKey),
		"every key here names a status; got %+v", diags)
}

func TestResponses_ErrorHeadersPreserved(t *testing.T) {
	t.Parallel()
	// ErrorCase has no Headers field; a 429's Retry-After header must not be
	// dropped silently — it is kept verbatim under Unmodeled with a diag.
	spec := pathsSpec(`  /w:
    get:
      operationId: w
      responses:
        "200": {description: ok}
        "429":
          description: slow down
          headers:
            Retry-After: {schema: {type: integer}}
`)
	_, svc, diags := lowerServiceSpec(t, spec)
	requireNoErrorDiags(t, diags)
	op := firstOp(t, svc)
	require.Len(t, op.Errors, 1)
	raw, ok := op.Errors[0].Unmodeled["openapi:headers"]
	require.True(t, ok, "error response headers kept under Unmodeled")
	assert.Contains(t, string(raw.Value), "Retry-After")
	assert.Equal(t, ir.ReasonNoIRHome, raw.Reason)

	found := false
	for _, d := range diags {
		if d.Severity == ir.SeverityInfo && strings.Contains(d.Message, "error response headers") {
			found = true
		}
	}
	assert.True(t, found, "dropped error headers emit one info diagnostic")
}

func TestOperation_ExplicitlyPublicSecurity(t *testing.T) {
	t.Parallel()
	spec := pathsSpec(`  /open:
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
	requireNoErrorDiags(t, diags)
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

func TestResponses_HeadersLowered(t *testing.T) {
	t.Parallel()
	spec := pathsSpec(`  /h:
    get:
      operationId: h
      responses:
        "200":
          description: ok
          headers:
            X-Rate-Limit: {required: true, schema: {type: integer}}
`)
	_, svc, diags := lowerServiceSpec(t, spec)
	requireNoErrorDiags(t, diags)
	op := firstOp(t, svc)
	require.Len(t, op.Responses, 1)
	require.Len(t, op.Responses[0].Headers, 1)
	h := op.Responses[0].Headers[0]
	assert.Equal(t, "X-Rate-Limit", h.WireName)
	assert.True(t, h.Required)
	assert.Equal(t, ir.TypeID("t/prim/integer"), h.Type.Target)
	assert.Equal(t, ir.PropID("p/openapi/paths/~1h/get/responses/200/headers/X-Rate-Limit"), h.ID)
}

// TestParameters_PathItemSharedAcrossOperationsInternsOnce is the fix's core
// scenario (issue #36): a path-item-level parameter merged into two sibling
// operations must intern its schema once, at the path item's own pointer —
// not once per operation under a pointer fabricated from that operation's
// unrelated parameter count.
func TestParameters_PathItemSharedAcrossOperationsInternsOnce(t *testing.T) {
	t.Parallel()
	spec := pathsSpec(`  /pets/{petId}:
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
	requireNoErrorDiags(t, diags)
	getPet := findOp(t, doc, "getPet")
	deletePet := findOp(t, doc, "deletePet")
	require.Len(t, getPet.Params, 1, "get inherits only the shared path-item parameter")
	require.Len(t, deletePet.Params, 2, "delete gets its own force plus the shared path-item parameter")

	wantID := ir.TypeID("t/anon/paths/~1pets~1{petId}/parameters/0/schema")
	assert.Equal(t, wantID, getPet.Params[0].Type.Target, "get resolves the shared path-item schema")

	byName := indexBy(deletePet.Params, func(p ir.Parameter) string { return p.Name.Source })
	assert.Equal(t, wantID, byName["petId"].Type.Target, "delete resolves the same shared schema, not a copy")

	typeDef, ok := doc.Types[wantID]
	require.True(t, ok, "the shared schema is registered under the path item's own pointer")
	assert.Equal(t, "/paths/~1pets~1{petId}/parameters/0/schema", typeDef.Common().Provenance.Pointer)

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
	before := pathsSpec(`  /pets/{petId}:
    parameters:
      - name: petId
        in: path
        required: true
        schema: {type: string, enum: [a, b]}
    get:
      operationId: getPet
      responses: {"200": {description: ok}}
`)
	after := pathsSpec(`  /pets/{petId}:
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
	requireNoErrorDiags(t, diagsBefore)
	_, svcAfter, diagsAfter := lowerServiceSpec(t, after)
	requireNoErrorDiags(t, diagsAfter)

	opBefore := firstOp(t, svcBefore)
	opAfter := firstOp(t, svcAfter)
	require.Len(t, opBefore.Params, 1)
	require.Len(t, opAfter.Params, 2, "the unrelated verbose parameter adds a second entry")

	byName := indexBy(opAfter.Params, func(p ir.Parameter) string { return p.Name.Source })
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
	requireNoErrorDiags(t, diags)
	op := findOp(t, doc, "onPetEvent")
	require.Len(t, op.Params, 1)

	wantID := ir.TypeID("t/anon/webhooks/petEvent/parameters/0/schema")
	assert.Equal(t, wantID, op.Params[0].Type.Target)
	typeDef, ok := doc.Types[wantID]
	require.True(t, ok)
	assert.Equal(t, "/webhooks/petEvent/parameters/0/schema", typeDef.Common().Provenance.Pointer)
}

// TestParameters_CallbackPathItemParameterPointer covers the same merge
// through lowerCallbackOps: a callback path-item-level parameter must hoist at
// the callback path item's own pointer, nested under the parent operation and
// callback expression.
func TestParameters_CallbackPathItemParameterPointer(t *testing.T) {
	t.Parallel()
	spec := pathsSpec(`  /subscribe:
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
	requireNoErrorDiags(t, diags)
	cbOp := findOp(t, doc, "onEvent")
	require.Len(t, cbOp.Params, 1)

	wantPointer := "/paths/~1subscribe/post/callbacks/onEvent/{$request.body#~1callbackUrl}/parameters/0/schema"
	wantID := ir.TypeID("t/anon" + wantPointer)
	assert.Equal(t, wantID, cbOp.Params[0].Type.Target)
	typeDef, ok := doc.Types[wantID]
	require.True(t, ok)
	assert.Equal(t, wantPointer, typeDef.Common().Provenance.Pointer)
}

// TestParameters_ShadowedPathItemParamUsesOperationPointer covers the other
// side of the merge: when the operation redeclares a path-item parameter, the
// operation's own schema must be used and lowered at the operation's own
// pointer — the shadowed path-item schema is never lowered at all.
func TestParameters_ShadowedPathItemParamUsesOperationPointer(t *testing.T) {
	t.Parallel()
	spec := pathsSpec(`  /pets/{petId}:
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
	requireNoErrorDiags(t, diags)
	op := firstOp(t, svc)
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

func TestResponses_LinksPreserved(t *testing.T) {
	t.Parallel()
	spec := pathsSpec(`  /l:
    get:
      operationId: l
      responses:
        "200":
          description: ok
          links:
            GetUserByUserId: {operationId: getUser}
`)
	_, svc, diags := lowerServiceSpec(t, spec)
	requireNoErrorDiags(t, diags)
	op := firstOp(t, svc)
	require.Len(t, op.Responses, 1)
	raw, ok := op.Responses[0].Unmodeled["openapi:links"]
	require.True(t, ok, "response links preserved raw for later promotion")
	assert.Contains(t, string(raw.Value), "GetUserByUserId")
	assert.Equal(t, ir.ReasonNoIRHome, raw.Reason)
}

func TestGrouping_ByPathPrefixInferred(t *testing.T) {
	t.Parallel()
	spec := pathsSpec(`  /users/{id}:
    get: {operationId: getUser, responses: {"200": {description: ok}}}
  /orders:
    get: {operationId: listOrders, responses: {"200": {description: ok}}}
`)
	svc, diags := serviceWithGrouping(t, spec, lowering.GroupByPathPrefix)
	requireNoErrorDiags(t, diags)
	byName := indexBy(svc.Groups, func(g ir.OperationGroup) string { return g.Name.Source })
	_, hasUsers := byName["users"]
	_, hasOrders := byName["orders"]
	assert.True(t, hasUsers, "first path segment forms a group")
	assert.True(t, hasOrders)
	for _, g := range svc.Groups {
		for _, op := range g.Operations {
			assert.Equal(t, "group-path-prefix", op.Provenance.Inferred, "op %s must be marked inferred", op.ID)
		}
	}
}

func TestOperation_NoOperationIdHint(t *testing.T) {
	t.Parallel()
	spec := pathsSpec(`  /ping:
    get:
      responses: {"200": {description: ok}}
`)
	_, svc, diags := lowerServiceSpec(t, spec)
	requireNoErrorDiags(t, diags)
	op := firstOp(t, svc)
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

	getA := findOp(t, doc, "getA")
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
	assert.NotEmpty(t, findOp(t, doc, "cbPost").ID)
	_ = diags
}

func TestOperations_NoResponses(t *testing.T) {
	t.Parallel()
	doc, _ := parseFull(t, opsSpec)
	trace := findOp(t, doc, "traceA")
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
	op := findOp(t, doc, "onPing")
	require.NotEmpty(t, op.Bindings.HTTP)
	assert.True(t, op.Bindings.HTTP[0].IsWebhook)
}

func TestGrouping_PathPrefixRootPath(t *testing.T) {
	t.Parallel()
	spec := pathsSpec(`  /:
    get: {operationId: root, responses: {"200": {description: ok}}}
`)
	svc, _ := serviceWithGrouping(t, spec, lowering.GroupByPathPrefix)
	require.NotEmpty(t, svc.Groups)
	assert.Equal(t, "", svc.Groups[0].Name.Source, "root path yields empty first segment")
}

func TestRawChildNode(t *testing.T) {
	t.Parallel()
	assert.Nil(t, annotation.RawChildNode(nil, "x"), "nil root")
	assert.Nil(t, annotation.RawChildNode(strNode("x"), "k"), "non-mapping root")

	var doc yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte("a: 1\nb: 2"), &doc))
	// doc is a DocumentNode wrapping the mapping — exercises the unwrap branch.
	got := annotation.RawChildNode(&doc, "b")
	require.NotNil(t, got)
	assert.Equal(t, "2", got.Value)
	assert.Nil(t, annotation.RawChildNode(&doc, "missing"), "absent key")
}

const componentResponseRefSpec = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a:
    get:
      operationId: getA
      responses:
        "200": {$ref: '#/components/responses/OK'}
  /b:
    get:
      operationId: getB
      responses:
        "200": {$ref: '#/components/responses/OK'}
components:
  responses:
    OK:
      description: ok
      content:
        application/json:
          schema: {type: object, properties: {n: {type: string}}}
`

// TestResponses_ComponentRefSharedAcrossOperationsInternsOnce is the fix's
// core scenario for a referenced response component (issue #107): two
// operations $ref'ing one #/components/responses/OK must intern its content
// schema once, at the component's own declaration pointer.
func TestResponses_ComponentRefSharedAcrossOperationsInternsOnce(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, componentResponseRefSpec)
	requireNoErrorDiags(t, diags)
	getA := findOp(t, doc, "getA")
	getB := findOp(t, doc, "getB")
	require.Len(t, getA.Responses, 1)
	require.Len(t, getB.Responses, 1)

	wantID := ir.TypeID("t/anon/components/responses/OK/content/application~1json/schema")
	assert.Equal(t, wantID, getA.Responses[0].Payload.Contents[0].Type.Target)
	assert.Equal(t, wantID, getB.Responses[0].Payload.Contents[0].Type.Target,
		"both operations resolve the same shared response schema")

	_, fabricatedA := doc.Types[ir.TypeID("t/anon/paths/~1a/get/responses/200/content/application~1json/schema")]
	_, fabricatedB := doc.Types[ir.TypeID("t/anon/paths/~1b/get/responses/200/content/application~1json/schema")]
	assert.False(t, fabricatedA, "no fabricated per-operation ID for /a")
	assert.False(t, fabricatedB, "no fabricated per-operation ID for /b")
}

const sharedPathItemSpec = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a: {$ref: '#/components/pathItems/Shared'}
  /b: {$ref: '#/components/pathItems/Shared'}
components:
  pathItems:
    Shared:
      parameters:
        - {name: shared, in: query, schema: {type: string, enum: [a, b]}}
      get:
        operationId: sharedGet
        requestBody:
          content:
            application/json:
              schema: {type: object, properties: {n: {type: string}}}
        responses:
          "200":
            description: ok
            content:
              application/json:
                schema: {type: object, properties: {m: {type: string}}}
`

// TestPathItem_RefSharedAcrossMountsKeepsDistinctOpIDs is the fix's core
// scenario for a referenced path item (issue #107): two paths mounting one
// #/components/pathItems/Shared must keep distinct operation identities —
// they are different URI templates, so they must never collide on one OpID
// (issue #36's risk otherwise) — while the shared parameter, request, and
// response schemas each intern once, at the shared component's own pointer.
func TestPathItem_RefSharedAcrossMountsKeepsDistinctOpIDs(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, sharedPathItemSpec)
	requireNoErrorDiags(t, diags)
	opA := opByPath(t, doc, "GET", "/a")
	opB := opByPath(t, doc, "GET", "/b")

	assert.Equal(t, ir.OpID("op/openapi/paths/~1a/get"), opA.ID)
	assert.Equal(t, ir.OpID("op/openapi/paths/~1b/get"), opB.ID)
	assert.NotEqual(t, opA.ID, opB.ID, "operations keep use-site identity")

	require.Len(t, opA.Params, 1)
	require.Len(t, opB.Params, 1)
	wantParamID := ir.TypeID("t/anon/components/pathItems/Shared/parameters/0/schema")
	assert.Equal(t, wantParamID, opA.Params[0].Type.Target)
	assert.Equal(t, wantParamID, opB.Params[0].Type.Target, "the shared path-item parameter interns once")

	require.NotNil(t, opA.Request)
	require.NotNil(t, opB.Request)
	wantReqID := ir.TypeID("t/anon/components/pathItems/Shared/get/requestBody/content/application~1json/schema")
	assert.Equal(t, wantReqID, opA.Request.Contents[0].Type.Target)
	assert.Equal(t, wantReqID, opB.Request.Contents[0].Type.Target, "the shared inline request schema interns once")

	require.Len(t, opA.Responses, 1)
	require.Len(t, opB.Responses, 1)
	wantRespID := ir.TypeID("t/anon/components/pathItems/Shared/get/responses/200/content/application~1json/schema")
	assert.Equal(t, wantRespID, opA.Responses[0].Payload.Contents[0].Type.Target)
	assert.Equal(t, wantRespID, opB.Responses[0].Payload.Contents[0].Type.Target,
		"the shared inline response schema interns once")
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

// TestCallbacks_RefSharedAcrossParentsKeepsDistinctOpIDs is the fix's core
// scenario for a referenced callback (issue #107): two parent operations
// $ref'ing one #/components/callbacks/Shared must keep distinct callback
// operation identities per parent while the callback's own request schema
// interns once, at the shared component's own pointer.
func TestCallbacks_RefSharedAcrossParentsKeepsDistinctOpIDs(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, sharedCallbackSpec)
	requireNoErrorDiags(t, diags)
	parentC := findOp(t, doc, "parentC")
	parentD := findOp(t, doc, "parentD")
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
// exists because findOp disambiguates by Name.Source, which two mounts of one
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
	requireNoErrorDiags(t, diags)

	var root yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte(provenanceSpec), &root))
	check := func(kind, name, pointer string) int {
		if pointer == "" {
			return 0
		}
		assert.True(t, pointerResolves(&root, pointer),
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
// node in a parsed YAML document: each segment (unescaped ~1 then ~0, via the
// production ids.UnescapeSegment) is followed as a mapping key, or, in a
// sequence, a decimal index. The empty pointer is skipped by callers, not
// treated as resolving here.
func pointerResolves(root *yaml.Node, pointer string) bool {
	node := root
	if node.Kind == yaml.DocumentNode && len(node.Content) > 0 {
		node = node.Content[0]
	}
	for raw := range strings.SplitSeq(strings.TrimPrefix(pointer, "/"), "/") {
		next, ok := pointerStep(node, ids.UnescapeSegment(raw))
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
	requireNoErrorDiags(t, diags)

	idA := ir.OpID("op/openapi/webhooks/onA/post")
	idB := ir.OpID("op/openapi/webhooks/onB/post")
	ops := opsByID(t, doc, idA, idB)
	require.Contains(t, ops, idA, "each webhook keeps its own mount identity")
	require.Contains(t, ops, idB, "each webhook keeps its own mount identity")
	assert.True(t, ops[idA].Bindings.HTTP[0].IsWebhook)

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
	requireNoErrorDiags(t, diags)
	parentC := findOp(t, doc, "parentC")
	parentD := findOp(t, doc, "parentD")
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

const refdErrorResponseSpec = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a:
    get:
      operationId: getA
      responses:
        "404": {$ref: '#/components/responses/NotFound'}
        default: {$ref: '#/components/responses/Fallback'}
  /b:
    get:
      operationId: getB
      responses:
        "404": {$ref: '#/components/responses/NotFound'}
        default: {$ref: '#/components/responses/Fallback'}
components:
  responses:
    NotFound:
      description: nope
      content:
        application/json:
          schema: {type: object, properties: {code: {type: string}}}
    Fallback:
      description: fallback
      content:
        application/json:
          schema: {type: object, properties: {msg: {type: string}}}
`

// TestResponses_RefdErrorAndDefaultInternAtDeclaration covers the two error
// arms of lowerResponses, which the success-response test does not reach: a
// $ref'd 4XX response (the isErrorRange branch) and a $ref'd default response,
// which resolves outside the status loop entirely. Both must intern their
// error models at the component's own pointer, once for the two operations.
func TestResponses_RefdErrorAndDefaultInternAtDeclaration(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, refdErrorResponseSpec)
	requireNoErrorDiags(t, diags)
	getA := findOp(t, doc, "getA")
	getB := findOp(t, doc, "getB")
	require.Len(t, getA.Errors, 2)
	require.Len(t, getB.Errors, 2)

	byFault := func(op ir.Operation) map[string]ir.ErrorCase {
		return indexBy(op.Errors, func(ec ir.ErrorCase) string { return ec.Fault })
	}
	aErrs, bErrs := byFault(getA), byFault(getB)

	wantNotFound := ir.TypeID("t/anon/components/responses/NotFound/content/application~1json/schema")
	assert.Equal(t, wantNotFound, aErrs["client"].Type.Target)
	assert.Equal(t, wantNotFound, bErrs["client"].Type.Target, "the shared 4XX error model interns once")

	// The default response is the unclassified catch-all, so it keys on "".
	wantFallback := ir.TypeID("t/anon/components/responses/Fallback/content/application~1json/schema")
	assert.Equal(t, wantFallback, aErrs[""].Type.Target)
	assert.Equal(t, wantFallback, bErrs[""].Type.Target, "the shared default error model interns once")

	for id := range doc.Types {
		assert.NotContains(t, string(id), "/responses/404", "no fabricated per-operation error ID")
		assert.NotContains(t, string(id), "/responses/default", "no fabricated per-operation default ID")
	}
}

const sharedOptionalBodySpec = `openapi: 3.1.0
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
          schema: {type: object, properties: {n: {type: string}}}
  responses:
    Err:
      description: err
      headers:
        X-E: {schema: {type: string}}
      content:
        application/json:
          schema: {type: object}
`

// TestDiag_SharedDeclarationReportsEachDefectOnce pins the consequence of
// lowering a referenced component at its declaration: both operations reach the
// same optional body and the same header-bearing error response, so each defect
// now has one pointer and one message. Reported per use site they would arrive
// as byte-identical copies — nothing a reader could act on twice — and a
// component shared by twenty operations would repeat each line twenty times.
func TestDiag_SharedDeclarationReportsEachDefectOnce(t *testing.T) {
	t.Parallel()
	_, diags := parseFull(t, sharedOptionalBodySpec)

	seen := map[string]int{}
	for _, d := range diags {
		seen[string(d.Severity)+"|"+d.Code+"|"+d.Provenance.Pointer+"|"+d.Message]++
	}
	for key, n := range seen {
		assert.Equal(t, 1, n, "one defect, one diagnostic: %s", key)
	}

	// Every defect still surfaces — de-duplication must not silence any of them.
	assert.Equal(t, 3, countDiagsAt(diags, diag.DegradedConstruct, ir.SeverityInfo),
		"the optional body, the homeless error headers and the homeless error media type "+
			"are three distinct defects")
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

	require.Equal(t, 1, countDiagsAt(diags, diag.DuplicateOperationID, ir.SeverityWarning),
		"the second claim is reported once, not both claims")
	for _, d := range diags {
		if d.Code == diag.DuplicateOperationID {
			assert.Equal(t, "/paths/~1b/get", d.Provenance.Pointer, "reported at the mount that collided")
			assert.Contains(t, d.Message, "/paths/~1a/get", "and names the mount that claimed it first")
		}
	}
}

// TestOperations_DistinctOperationIDsClean is the control: ordinary operations
// with their own ids, and an operation with none at all, raise nothing.
func TestOperations_DistinctOperationIDsClean(t *testing.T) {
	t.Parallel()
	_, diags := parseFull(t, pathsSpec(`  /a:
    get:
      operationId: getA
      responses: {"200": {description: ok}}
  /b:
    get:
      responses: {"200": {description: ok}}
`))
	assert.False(t, hasDiag(diags, diag.DuplicateOperationID))
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
	spec := pathsSpec(`  /x:
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

	// And the skips themselves are silent: every diagnostic here is the resolve
	// phase's own report of a component that does not exist, which is why none
	// carries a pointer — that phase runs before the walk that would know one. A
	// skip lowering an empty stand-in instead would report on a construct the
	// document never wrote, and would report it from the walk, sited.
	for _, d := range diags {
		assert.Equal(t, diag.UnresolvedRef, d.Code, "%+v", d)
		assert.Empty(t, d.Provenance.Pointer, "reported by the resolve phase, not the walk: %+v", d)
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
	doc, diags := parseFull(t, pathsSpec(`  /a:
    get: {operationId: getA, deprecated: true, responses: {"200": {description: ok}}}
    post: {operationId: postA, responses: {"200": {description: ok}}}
`))
	requireNoErrorDiags(t, diags)

	assert.NotNil(t, findOp(t, doc, "getA").Deprecation, "the declared flag is carried")
	assert.Nil(t, findOp(t, doc, "postA").Deprecation, "and an operation that declares none has none")
}

// pathItemServersSpec declares the same `servers` override on each of the three
// path items a document can hold — a path, a webhook, and a callback expression
// — with a distinct URL apiece so a preserved list can be traced to the path
// item that wrote it.
const pathItemServersSpec = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /p:
    servers: [{url: 'https://path.example'}]
    post:
      operationId: postP
      callbacks:
        onEvent:
          '{$request.body#/url}':
            servers: [{url: 'https://callback.example'}]
            post:
              operationId: onEvent
              responses: {"200": {description: ok}}
      responses: {"200": {description: ok}}
webhooks:
  hooked:
    servers: [{url: 'https://webhook.example'}]
    post:
      operationId: onHook
      responses: {"200": {description: ok}}
`

// TestOperations_PathItemServersKeptOnEveryRoute pins that a path item's servers
// survive whichever of the three routes reaches the path item.
//
// A path item is one object with three parents, and only the `paths` walk called
// the preserve-plus-diagnostic path: a webhook or callback that overrode its
// delivery host lost the override with nothing said, while the identical
// declaration under `paths` was both kept and reported. Each route is asserted
// with its own URL, so a fix that preserved the wrong path item's list — or the
// enclosing one's — fails rather than passing on the shape alone.
func TestOperations_PathItemServersKeptOnEveryRoute(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, pathItemServersSpec)
	requireNoErrorDiags(t, diags)

	// kept is where the preserved list is recorded (the servers keyword);
	// reported is where the diagnostic is stamped (the operation itself).
	for _, tc := range []struct{ op, url, kept, reported string }{
		{"postP", "https://path.example", "/paths/~1p/servers", "/paths/~1p/post"},
		{"onHook", "https://webhook.example", "/webhooks/hooked/servers", "/webhooks/hooked/post"},
		{"onEvent", "https://callback.example",
			"/paths/~1p/post/callbacks/onEvent/{$request.body#~1url}/servers",
			"/paths/~1p/post/callbacks/onEvent/{$request.body#~1url}/post"},
	} {
		entry, ok := findOp(t, doc, tc.op).Unmodeled["openapi:servers"]
		require.True(t, ok, "%s keeps its path item's servers", tc.op)
		assert.Equal(t, ir.ReasonNoIRHome, entry.Reason)
		assert.JSONEq(t, `[{"url":"`+tc.url+`"}]`, string(entry.Value),
			"%s keeps the list its own path item declared", tc.op)
		assert.Equal(t, tc.kept, entry.Provenance.Pointer)
		assert.True(t, hasDiagCodeAt(diags, diag.DegradedConstruct, tc.reported),
			"%s reports the degradation as the paths route already did", tc.op)
	}
}

// TestErrorCase_SingleMediaTypeKeepsContentMap pins the arity-independent half of
// error-content preservation. ir.ErrorCase holds a TypeRef and no media type, so
// an error declared only as application/problem+json reached the IR
// indistinguishable from one declared as application/json — while the same
// response with a second media type beside it was kept in full. One entry losing
// its key is the same loss as several losing all but the first.
func TestErrorCase_SingleMediaTypeKeepsContentMap(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, pathsSpec(`  /x:
    get:
      operationId: getX
      responses:
        "200": {description: ok}
        "404":
          description: gone
          content:
            application/problem+json:
              schema: {type: object}
        "409": {description: conflict}
`))
	requireNoErrorDiags(t, diags)
	errs := indexBy(findOp(t, doc, "getX").Errors,
		func(ec ir.ErrorCase) int { return ec.Conditions.StatusCodes[0].From })

	entry, ok := errs[404].Unmodeled["openapi:content"]
	require.True(t, ok, "the single-entry content map is kept")
	assert.Equal(t, ir.ReasonNoIRHome, entry.Reason)
	assert.JSONEq(t, `{"application/problem+json":{"schema":{"type":"object"}}}`, string(entry.Value),
		"the media type the map is keyed by is what would otherwise be lost")
	assert.Equal(t, "/paths/~1x/get/responses/404/content", entry.Provenance.Pointer)
	assert.Contains(t,
		diagMessageAt(t, diags, diag.DegradedConstruct, ir.SeverityInfo, "/paths/~1x/get/responses/404"),
		"media type has no ErrorCase home",
		"the single-entry case names its own loss, not the multi-entry one")

	assert.NotContains(t, errs[409].Unmodeled, "openapi:content",
		"an error response declaring no content keeps no content map")
}

// operationServersSpec declares `servers` at both levels OpenAPI allows on one
// operation, with a distinct URL apiece, plus an operation that declares only
// its own so the fallback shape is covered too.
const operationServersSpec = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /both:
    servers: [{url: 'https://pathitem.example'}]
    get:
      operationId: getBoth
      servers: [{url: 'https://operation.example'}]
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
`

// TestOperations_OwnServersKeptBesideThePathItems pins the overriding half of
// the servers pair. OpenAPI says an Operation Object's servers override the Path
// Item Object's, but only the path item's were read: a document declaring both
// kept the superseded list and dropped the effective one outright, so an emitter
// reading openapi:servers would route to a host the operation had replaced —
// and nothing reported it.
//
// The two are asserted under separate keys because they are two declarations at
// two pointers. One key for both would make the surviving list depend on which
// lowering ran last, which no single-order test could see.
func TestOperations_OwnServersKeptBesideThePathItems(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, operationServersSpec)
	requireNoErrorDiags(t, diags)

	both := findOp(t, doc, "getBoth")
	own, ok := both.Unmodeled["openapi:operationServers"]
	require.True(t, ok, "the operation's own servers are kept")
	assert.Equal(t, ir.ReasonNoIRHome, own.Reason)
	assert.JSONEq(t, `[{"url":"https://operation.example"}]`, string(own.Value),
		"and they are the operation's list, not the path item's")
	assert.Equal(t, "/paths/~1both/get/servers", own.Provenance.Pointer)

	inherited, ok := both.Unmodeled["openapi:servers"]
	require.True(t, ok, "the path item's list is kept beside it, not replaced by it")
	assert.JSONEq(t, `[{"url":"https://pathitem.example"}]`, string(inherited.Value))
	assert.Equal(t, "/paths/~1both/servers", inherited.Provenance.Pointer,
		"each entry keeps the coordinate of the object that declared it")

	assert.True(t, hasDiagCodeAt(diags, diag.DegradedConstruct, "/paths/~1both/get"),
		"the operation's own list is reported as the path item's already was")
}

// TestOperations_ServersKeysAreIndependent is the control for the test above:
// each key records only what its own object declared, so an operation declaring
// one level records that level alone rather than both.
func TestOperations_ServersKeysAreIndependent(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, operationServersSpec)
	requireNoErrorDiags(t, diags)

	opOnly := findOp(t, doc, "getOperationOnly")
	assert.Contains(t, opOnly.Unmodeled, "openapi:operationServers")
	assert.NotContains(t, opOnly.Unmodeled, "openapi:servers",
		"an operation whose path item declares none records none for it")

	pathOnly := findOp(t, doc, "getPathItemOnly")
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
	doc, diags := parseFull(t, pathsSpec(`  /x:
    get:
      operationId: getX
      servers: [{url: 'https://operation.example'}]
      x-vendor: kept
      responses: {"200": {description: ok}}
`))
	requireNoErrorDiags(t, diags)

	op := findOp(t, doc, "getX")
	servers, ok := op.Unmodeled["openapi:operationServers"]
	require.True(t, ok, "the servers survive the extensions assignment")
	assert.JSONEq(t, `[{"url":"https://operation.example"}]`, string(servers.Value))
	assert.Contains(t, op.Unmodeled, "openapi:x-vendor",
		"and the extensions survive beside them, so neither overwrote the other")
}
