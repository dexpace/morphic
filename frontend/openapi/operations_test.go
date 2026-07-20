package openapi

import (
	"testing"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/frontend"
	"github.com/dexpace/morphic/ir"
)

// lowerServiceSpec lowers components and the service layer of src.
func lowerServiceSpec(t *testing.T, src string) (*ir.Document, ir.Service, []ir.Diagnostic) {
	t.Helper()
	doc, diags := func() (*ir.Document, []ir.Diagnostic) {
		loadedDoc, loadDiags, err := load(t.Context(), 0, frontend.Source{Path: "spec.yaml", Data: []byte(src)}, Options{}.withDefaults())
		require.NoError(t, err)
		require.NotNil(t, loadedDoc)
		l := newLowerer(0, loadedDoc, Options{}.withDefaults())
		l.lowerComponentSchemas()
		l.lowerSecuritySchemes()
		l.out.Services = []ir.Service{l.lowerService()}
		return l.out, append(loadDiags, l.diags...)
	}()
	require.Len(t, doc.Services, 1)
	return doc, doc.Services[0], diags
}

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
	byName := map[string]ir.OperationGroup{}
	for _, g := range svc.Groups {
		byName[g.Name.Source] = g
	}
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
	spec := `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /w:
    get:
      operationId: w
      responses:
        "200": {description: ok}
        "404":
          description: missing
          content: {application/json: {schema: {type: object, properties: {msg: {type: string}}}}}
        "5XX": {description: server oops}
        default: {description: anything else}
`
	_, svc, diags := lowerServiceSpec(t, spec)
	requireNoErrorDiags(t, diags)
	op := svc.Groups[0].Operations[0]
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

func TestOperation_ExplicitlyPublicSecurity(t *testing.T) {
	t.Parallel()
	spec := `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /open:
    get:
      operationId: open
      security: []
      responses: {"200": {description: ok}}
  /inherits:
    get:
      operationId: inherits
      responses: {"200": {description: ok}}
`
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
		if g.Name.Source == "webhooks" {
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
	spec := `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /h:
    get:
      operationId: h
      responses:
        "200":
          description: ok
          headers:
            X-Rate-Limit: {required: true, schema: {type: integer}}
`
	_, svc, diags := lowerServiceSpec(t, spec)
	requireNoErrorDiags(t, diags)
	op := svc.Groups[0].Operations[0]
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
	spec := `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /subscribe:
    post:
      operationId: sub
      callbacks:
        onEvent:
          '{$request.body#/cb}':
            post:
              operationId: cbPost
              responses: {"200": {description: ok}}
      responses: {"200": {description: ok}}
`
	_, svc, diags := lowerServiceSpec(t, spec)
	requireNoErrorDiags(t, diags)
	require.Len(t, svc.Groups, 1)
	group := svc.Groups[0]
	require.Len(t, group.Operations, 2, "parent op and callback op both registered")
	byName := map[string]ir.Operation{}
	for _, op := range group.Operations {
		byName[op.Name.Source] = op
	}
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
	spec := `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /users/{id}:
    parameters:
      - {name: id, in: path, required: true, schema: {type: string}, description: path-level}
      - {name: trace, in: header, schema: {type: string}}
    get:
      operationId: g
      parameters:
        - {name: id, in: path, required: true, schema: {type: integer}, description: op-level}
      responses: {"200": {description: ok}}
`
	loadedDoc, _, err := load(t.Context(), 0, frontend.Source{Path: "spec.yaml", Data: []byte(spec)}, Options{}.withDefaults())
	require.NoError(t, err)
	require.NotNil(t, loadedDoc)
	var pi *soa.PathItem
	for _, rp := range loadedDoc.Doc.GetPaths().All() {
		pi = resolvePathItem(rp)
	}
	require.NotNil(t, pi)
	op := pi.Get()
	require.NotNil(t, op)
	merged := mergeParameters(pi.GetParameters(), op.GetParameters())
	require.Len(t, merged, 2, "shared (name,in) collapses to one; op wins")
	assert.Same(t, op.GetParameters()[0], merged[0], "operation parameter overrides the path-item one")
	names := map[string]bool{}
	for _, p := range merged {
		names[resolveParameter(p).GetName()] = true
	}
	assert.True(t, names["id"])
	assert.True(t, names["trace"])
}

func TestResponses_LinksPreserved(t *testing.T) {
	t.Parallel()
	spec := `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /l:
    get:
      operationId: l
      responses:
        "200":
          description: ok
          links:
            GetUserByUserId: {operationId: getUser}
`
	_, svc, diags := lowerServiceSpec(t, spec)
	requireNoErrorDiags(t, diags)
	op := svc.Groups[0].Operations[0]
	require.Len(t, op.Responses, 1)
	raw, ok := op.Responses[0].Extensions["openapi:links"]
	require.True(t, ok, "response links preserved raw for later promotion")
	assert.Contains(t, string(raw), "GetUserByUserId")
}

func TestGrouping_ByPathPrefixInferred(t *testing.T) {
	t.Parallel()
	spec := `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /users/{id}:
    get: {operationId: getUser, responses: {"200": {description: ok}}}
  /orders:
    get: {operationId: listOrders, responses: {"200": {description: ok}}}
`
	opts := Options{Grouping: GroupByPathPrefix}.withDefaults()
	loadedDoc, loadDiags, err := load(t.Context(), 0, frontend.Source{Path: "spec.yaml", Data: []byte(spec)}, opts)
	require.NoError(t, err)
	require.NotNil(t, loadedDoc)
	l := newLowerer(0, loadedDoc, opts)
	l.lowerComponentSchemas()
	svc := l.lowerService()
	requireNoErrorDiags(t, append(loadDiags, l.diags...))
	byName := map[string]ir.OperationGroup{}
	for _, g := range svc.Groups {
		byName[g.Name.Source] = g
	}
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
	spec := `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /ping:
    get:
      responses: {"200": {description: ok}}
`
	_, svc, diags := lowerServiceSpec(t, spec)
	requireNoErrorDiags(t, diags)
	op := svc.Groups[0].Operations[0]
	assert.Empty(t, op.Name.Source, "no operationId leaves an empty source name")
	assert.Equal(t, canonicalWords("get /ping"), op.Name.Hint)
}
