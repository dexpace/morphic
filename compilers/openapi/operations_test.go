package openapi

import (
	"os"
	"strconv"
	"strings"
	"testing"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/speakeasy-api/openapi/sequencedmap"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/load"
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

func TestWebhooks_WebhookGroup(t *testing.T) {
	t.Parallel()
	spec := `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
webhooks:
  newPet:
    post:
      operationId: onNewPet
      responses: {"200": {description: ok}}
`
	_, svc, diags := lowerServiceSpec(t, spec)
	requireNoErrorDiags(t, diags)
	var group ir.OperationGroup
	found := false
	for _, g := range svc.Groups {
		if g.Name.Hint == "webhooks" {
			group, found = g, true
		}
	}
	require.True(t, found, "webhook operations land in the webhooks group")
	require.Len(t, group.Operations, 1)
	op := group.Operations[0]
	assert.Equal(t, ir.OpID("op/openapi/webhooks/newPet/post"), op.ID)
	require.Len(t, op.Bindings.HTTP, 1)
	assert.True(t, op.Bindings.HTTP[0].IsWebhook)
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

func TestCallbacks_RegisteredAndBound(t *testing.T) {
	t.Parallel()
	spec := pathsSpec(`  /subscribe:
    post:
      operationId: sub
      callbacks:
        onEvent:
          '{$request.body#/cb}':
            post:
              operationId: cbPost
              responses: {"200": {description: ok}}
      responses: {"200": {description: ok}}
`)
	_, svc, diags := lowerServiceSpec(t, spec)
	requireNoErrorDiags(t, diags)
	require.Len(t, svc.Groups, 1)
	group := svc.Groups[0]
	require.Len(t, group.Operations, 2, "parent op and callback op both registered")
	byName := indexBy(group.Operations, func(op ir.Operation) string { return op.Name.Source })
	sub, ok := byName["sub"]
	require.True(t, ok)
	cb, ok := byName["cbPost"]
	require.True(t, ok)
	require.Len(t, sub.Bindings.HTTP, 1)
	require.Len(t, sub.Bindings.HTTP[0].Callbacks, 1)
	call := sub.Bindings.HTTP[0].Callbacks[0]
	assert.Equal(t, "{$request.body#/cb}", call.Expression)
	require.Len(t, call.Operations, 1)
	assert.Equal(t, cb.ID, call.Operations[0])
}

func TestParameters_PathItemMergeOverride(t *testing.T) {
	t.Parallel()
	spec := pathsSpec(`  /users/{id}:
    parameters:
      - {name: id, in: path, required: true, schema: {type: string}, description: path-level}
      - {name: trace, in: header, schema: {type: string}}
    get:
      operationId: g
      parameters:
        - {name: id, in: path, required: true, schema: {type: integer}, description: op-level}
      responses: {"200": {description: ok}}
`)
	loadedDoc, _, err := load.Load(t.Context(), 0, compilers.Source{Path: "spec.yaml", Data: []byte(spec)}, loadOptions(Options{}.withDefaults()))
	require.NoError(t, err)
	require.NotNil(t, loadedDoc)
	var pi *soa.PathItem
	for _, rp := range loadedDoc.Doc.GetPaths().All() {
		pi = resolveRef[soa.PathItem](rp)
	}
	require.NotNil(t, pi)
	op := pi.Get()
	require.NotNil(t, op)
	pathPtr := ids.Ptr("paths", "/users/{id}")
	opPtr := pathPtr + ids.Ptr("get")
	merged := mergeParameters(pi.GetParameters(), op.GetParameters(), pathPtr, opPtr)
	require.Len(t, merged, 2, "shared (name,in) collapses to one; op wins")
	assert.Same(t, op.GetParameters()[0], merged[0].ref, "operation parameter overrides the path-item one")
	assert.Equal(t, "/paths/~1users~1{id}/get/parameters/0", merged[0].pointer,
		"the op-level parameter keeps its own declaration pointer")

	byName := map[string]sourcedParam{}
	for _, sp := range merged {
		byName[resolveRef[soa.Parameter](sp.ref).GetName()] = sp
	}
	_, hasID := byName["id"]
	_, hasTrace := byName["trace"]
	assert.True(t, hasID)
	assert.True(t, hasTrace)
	assert.Equal(t, "/paths/~1users~1{id}/parameters/1", byName["trace"].pointer,
		"the unshadowed path-level parameter keeps its own declaration pointer, at its own path-item index")
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
	opts := Options{Grouping: GroupByPathPrefix}.withDefaults()
	loadedDoc, loadDiags, err := load.Load(t.Context(), 0, compilers.Source{Path: "spec.yaml", Data: []byte(spec)}, loadOptions(opts))
	require.NoError(t, err)
	require.NotNil(t, loadedDoc)
	l := newLowerer(0, loadedDoc, opts)
	l.lowerComponentSchemas()
	svc := l.lowerService()
	requireNoErrorDiags(t, append(loadDiags, l.diags.List()...))
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
	opts := Options{Grouping: GroupByPathPrefix}.withDefaults()
	loadedDoc, _, err := load.Load(t.Context(), 0, sourceOf(spec), loadOptions(opts))
	require.NoError(t, err)
	l := newLowerer(0, loadedDoc, opts)
	l.lowerComponentSchemas()
	svc := l.lowerService()
	require.NotEmpty(t, svc.Groups)
	assert.Equal(t, "", svc.Groups[0].Name.Source, "root path yields empty first segment")
}

func TestStatusRange(t *testing.T) {
	t.Parallel()
	cases := []struct {
		code     string
		from, to int
	}{
		{"default", 0, 0},
		{"200", 200, 200},
		{"4XX", 400, 499},
		{"5xx", 500, 599},
		{"20A", 0, 0}, // non-numeric, non-range → catch-all
	}
	for _, tc := range cases {
		t.Run(tc.code, func(t *testing.T) {
			t.Parallel()
			r := statusRange(tc.code)
			assert.Equal(t, tc.from, r.From)
			assert.Equal(t, tc.to, r.To)
		})
	}
}

func TestFaultFor(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "client", faultFor(ir.StatusRange{From: 404, To: 404}))
	assert.Equal(t, "server", faultFor(ir.StatusRange{From: 503, To: 503}))
	assert.Equal(t, "", faultFor(ir.StatusRange{}))
}

func TestPreserveErrorHeaders_WithoutRootNode(t *testing.T) {
	t.Parallel()
	l := newRawLowerer(&soa.OpenAPI{})
	headers := sequencedmap.New(
		sequencedmap.NewElem("X-H", &soa.ReferencedHeader{}),
	)
	ec := &ir.ErrorCase{}
	l.preserveErrorHeaders(ec, &soa.Response{Headers: headers}, "/r")
	assert.Nil(t, ec.Unmodeled, "headers with no raw node are not preserved")
	require.Empty(t, l.diags.List())
}

func TestLowerResponses_NoResponses(t *testing.T) {
	t.Parallel()
	l := newRawLowerer(&soa.OpenAPI{})
	responses, errs := l.lowerResponses(&soa.Operation{}, "/op")
	assert.Nil(t, responses)
	assert.Nil(t, errs)
}

func TestFirstPathSegment_Empty(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "", firstPathSegment("/"))
	assert.Equal(t, "users", firstPathSegment("/users/{id}"))
}

func TestApplyPathServers_WithoutRootNode(t *testing.T) {
	t.Parallel()
	l := newRawLowerer(&soa.OpenAPI{})
	op := &ir.Operation{}
	l.applyPathServers(op, &soa.PathItem{Servers: []*soa.Server{{URL: "https://x"}}}, "/paths/~1a")
	assert.Nil(t, op.Unmodeled, "servers with no raw node are not preserved")
	assert.Empty(t, l.diags.List())
}

func TestLowerTagDefs_NilEntrySkipped(t *testing.T) {
	t.Parallel()
	l := newRawLowerer(&soa.OpenAPI{Tags: []*soa.Tag{nil, {}}})
	l.lowerTagDefs()
	assert.Len(t, l.out.TagDefs, 1, "nil tag entry skipped")
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

func TestResolvers_NilInputs(t *testing.T) {
	t.Parallel()
	var (
		rpi *soa.ReferencedPathItem
		rr  *soa.ReferencedResponse
		rh  *soa.ReferencedHeader
		rcb *soa.ReferencedCallback
		rp  *soa.ReferencedParameter
		rrb *soa.ReferencedRequestBody
		re  *soa.ReferencedExample
		rss *soa.ReferencedSecurityScheme
	)
	assert.Nil(t, resolveRef[soa.PathItem](rpi))
	assert.Nil(t, resolveRef[soa.Response](rr))
	assert.Nil(t, resolveRef[soa.Header](rh))
	assert.Nil(t, resolveRef[soa.Callback](rcb))
	assert.Nil(t, resolveRef[soa.Parameter](rp))
	assert.Nil(t, resolveRef[soa.RequestBody](rrb))
	assert.Nil(t, resolveRef[soa.Example](re))
	assert.Nil(t, resolveRef[soa.SecurityScheme](rss))
	_, ok := paramKey(nil)
	assert.False(t, ok)
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
func opsByID(t *testing.T, doc *ir.Document, ids ...ir.OpID) map[ir.OpID]ir.Operation {
	t.Helper()
	require.NotEmpty(t, doc.Services, "the spec lowers to at least one service")
	want := make(map[ir.OpID]bool, len(ids))
	for _, id := range ids {
		want[id] = true
	}
	out := make(map[ir.OpID]ir.Operation, len(ids))
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

// TestResolveRefAt_CrossDocumentKeepsUseSitePointer is the cross-document
// counterpart to the same-document sharing tests above (issue #107). The
// fixture's operation $refs a parameter and a response into a sibling
// document; both resolve to real objects (resolveAll follows external refs),
// but resolveRefAt's internal-pointer check rejects the target because it lives
// in another document, so the use-site pointer is kept rather than a pointer
// into a document this IR has no node for.
func TestResolveRefAt_CrossDocumentKeepsUseSitePointer(t *testing.T) {
	t.Parallel()
	doc := compileFixture(t, "../../testdata/openapi/resolve_main_external_valid.yaml")
	assertCrossDocumentUseSitePointers(t, doc)
}

// TestResolveRefAt_AliasChainLeavingDocumentKeepsUseSitePointer covers the hop
// the plain cross-document case does not: a chain that starts inside this
// document and only then leaves it (PageAlias -> ./target#/…/Page). The alias
// pointer walked past is itself a one-key $ref object with no schema child, so
// adopting it would fabricate a pointer just as surely as the use site did
// before issue #107 — the walk must fall all the way back to the use site,
// matching what a direct cross-document reference already does.
func TestResolveRefAt_AliasChainLeavingDocumentKeepsUseSitePointer(t *testing.T) {
	t.Parallel()
	doc := compileFixture(t, "../../testdata/openapi/resolve_main_alias_external_valid.yaml")
	assertCrossDocumentUseSitePointers(t, doc)
}

// compileFixture loads and lowers one on-disk spec, which — unlike parseFull's
// in-memory source — is what lets the loader resolve a sibling document.
func compileFixture(t *testing.T, path string) *ir.Document {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)

	opts := Options{}.withDefaults()
	loadedDoc, loadDiags, err := load.Load(t.Context(), 0, compilers.Source{Path: path, Data: data}, loadOptions(opts))
	require.NoError(t, err)
	require.NotNil(t, loadedDoc)

	l := newLowerer(0, loadedDoc, opts)
	doc := l.run()
	requireNoErrorDiags(t, append(loadDiags, l.diags.List()...))
	return doc
}

// assertCrossDocumentUseSitePointers pins the shared outcome of both
// cross-document fixtures: the schemas intern at the operation's own pointers,
// and nothing interns under a /components/ pointer — neither the sibling
// document's (this IR has no node there) nor a local alias's.
func assertCrossDocumentUseSitePointers(t *testing.T, doc *ir.Document) {
	t.Helper()
	wantParamID := ir.TypeID("t/anon/paths/~1a/get/parameters/0/schema")
	wantRespID := ir.TypeID("t/anon/paths/~1a/get/responses/200/content/application~1json/schema")
	_, hasParam := doc.Types[wantParamID]
	_, hasResp := doc.Types[wantRespID]
	assert.True(t, hasParam, "the cross-document parameter schema interns at the use-site pointer")
	assert.True(t, hasResp, "the cross-document response schema interns at the use-site pointer")

	for id := range doc.Types {
		assert.False(t, strings.HasPrefix(string(id), "t/anon/components/"),
			"no type interned under an unaddressable declaration pointer: %s", id)
	}
}

const aliasedComponentChainSpec = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a:
    get:
      operationId: getA
      parameters:
        - {$ref: '#/components/parameters/PageAlias'}
      responses:
        "200": {$ref: '#/components/responses/OKAlias'}
  /b:
    get:
      operationId: getB
      parameters:
        - {$ref: '#/components/parameters/PageAlias'}
      responses:
        "200": {$ref: '#/components/responses/OKAlias'}
components:
  parameters:
    PageAlias: {$ref: '#/components/parameters/Page'}
    Page: {name: page, in: query, schema: {type: string, enum: [p, q]}}
  responses:
    OKAlias: {$ref: '#/components/responses/OK'}
    OK:
      description: ok
      content:
        application/json:
          schema: {type: object, properties: {n: {type: string}}}
`

// TestResolveRefAt_AliasedComponentChainInternsAtFinalDeclaration guards a
// component that is itself a bare $ref to another component (PageAlias ->
// Page, OKAlias -> OK): resolveRefAt must walk the whole chain to Page's and
// OK's own declaration, not stop at the one-hop alias pointer, which is a
// one-key $ref object with no schema (or content) child of its own to hoist.
func TestResolveRefAt_AliasedComponentChainInternsAtFinalDeclaration(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, aliasedComponentChainSpec)
	requireNoErrorDiags(t, diags)
	getA := findOp(t, doc, "getA")
	getB := findOp(t, doc, "getB")

	require.Len(t, getA.Params, 1)
	require.Len(t, getB.Params, 1)
	wantParamID := ir.TypeID("t/anon/components/parameters/Page/schema")
	assert.Equal(t, wantParamID, getA.Params[0].Type.Target)
	assert.Equal(t, wantParamID, getB.Params[0].Type.Target,
		"both operations resolve the aliased parameter's final declaration")

	require.Len(t, getA.Responses, 1)
	require.Len(t, getB.Responses, 1)
	wantRespID := ir.TypeID("t/anon/components/responses/OK/content/application~1json/schema")
	assert.Equal(t, wantRespID, getA.Responses[0].Payload.Contents[0].Type.Target)
	assert.Equal(t, wantRespID, getB.Responses[0].Payload.Contents[0].Type.Target,
		"both operations resolve the aliased response's final declaration")

	for id := range doc.Types {
		assert.NotContains(t, string(id), "PageAlias", "no ID derived from the one-hop alias pointer")
		assert.NotContains(t, string(id), "OKAlias", "no ID derived from the one-hop alias pointer")
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

// chainedAliasSpec builds a spec whose operation $refs P0 at the head of an
// alias chain P0 -> P1 -> ... -> P<hops>, where only P<hops> is a real
// parameter declaration. The walk therefore takes hops+1 steps: one from the
// use site, then one per alias.
func chainedAliasSpec(hops int) string {
	var b strings.Builder
	b.WriteString(`openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a:
    get:
      operationId: getA
      parameters:
        - {$ref: '#/components/parameters/P0'}
      responses: {"200": {description: ok}}
components:
  parameters:
`)
	for i := range hops {
		b.WriteString("    P" + strconv.Itoa(i) + ": {$ref: '#/components/parameters/P" + strconv.Itoa(i+1) + "'}\n")
	}
	b.WriteString("    P" + strconv.Itoa(hops) + ": {name: page, in: query, schema: {type: string, enum: [a, b]}}\n")
	return b.String()
}

// TestResolveRefAt_AliasChainWithinBoundReachesDeclaration walks a chain
// several hops deeper than the two-hop regression above, to pin that the walk
// is genuinely iterative rather than a fixed one-or-two-hop peel.
func TestResolveRefAt_AliasChainWithinBoundReachesDeclaration(t *testing.T) {
	t.Parallel()
	const hops = 8
	doc, diags := parseFull(t, chainedAliasSpec(hops))
	requireNoErrorDiags(t, diags)
	op := findOp(t, doc, "getA")
	require.Len(t, op.Params, 1)
	assert.Equal(t, ir.TypeID("t/anon/components/parameters/P8/schema"), op.Params[0].Type.Target,
		"the walk follows every hop to the final declaration")
}

// TestResolveRefAt_AliasChainBeyondBoundFallsBackToUseSite pins the behaviour
// of maxRefChain itself. A chain longer than the bound has no declaration the
// walk can prove it reached, so it must fall back to the use-site pointer —
// terminating, deterministic, and never a mid-chain alias whose only key is
// $ref. Nothing in a real spec looks like this; the bound exists so a
// pathological one cannot make the walk unbounded.
func TestResolveRefAt_AliasChainBeyondBoundFallsBackToUseSite(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, chainedAliasSpec(maxRefChain+4))
	requireNoErrorDiags(t, diags)
	op := findOp(t, doc, "getA")
	require.Len(t, op.Params, 1)
	assert.Equal(t, ir.TypeID("t/anon/paths/~1a/get/parameters/0/schema"), op.Params[0].Type.Target,
		"an over-long chain keeps the one pointer that is certainly addressable")
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

	// Both defects still surface — de-duplication must not silence either.
	assert.Equal(t, 2, countDiagsAt(diags, diag.DegradedConstruct, ir.SeverityInfo),
		"the optional body and the homeless error headers are two distinct defects")
}

// TestDiag_DistinctDefectsAtOnePointerBothSurvive is the control for the rule
// above: de-duplication compares the whole diagnostic, so two different messages
// at one pointer are two findings, not one repeated.
func TestDiag_DistinctDefectsAtOnePointerBothSurvive(t *testing.T) {
	t.Parallel()
	l := newRawLowerer(nil)
	l.diag(ir.SeverityWarning, diag.DegradedConstruct, "/p", "first")
	l.diag(ir.SeverityWarning, diag.DegradedConstruct, "/p", "second")
	l.diag(ir.SeverityWarning, diag.DegradedConstruct, "/p", "first")
	require.Len(t, l.diags.List(), 2, "the repeat is dropped, the distinct message is not")
	assert.Equal(t, "first", l.diags.List()[0].Message)
	assert.Equal(t, "second", l.diags.List()[1].Message)
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
