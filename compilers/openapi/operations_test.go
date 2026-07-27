package openapi

import (
	"strings"
	"testing"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/speakeasy-api/openapi/sequencedmap"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers"
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
	// dropped silently — it is preserved verbatim under Extensions with a diag.
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
	raw, ok := op.Errors[0].Extensions["openapi:headers"]
	require.True(t, ok, "error response headers preserved under extensions")
	assert.Contains(t, string(raw), "Retry-After")

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
	loadedDoc, _, err := load(t.Context(), 0, compilers.Source{Path: "spec.yaml", Data: []byte(spec)}, Options{}.withDefaults())
	require.NoError(t, err)
	require.NotNil(t, loadedDoc)
	var pi *soa.PathItem
	for _, rp := range loadedDoc.Doc.GetPaths().All() {
		pi = resolveRef(rp)
	}
	require.NotNil(t, pi)
	op := pi.Get()
	require.NotNil(t, op)
	merged := mergeParameters(pi.GetParameters(), op.GetParameters())
	require.Len(t, merged, 2, "shared (name,in) collapses to one; op wins")
	assert.Same(t, op.GetParameters()[0], merged[0], "operation parameter overrides the path-item one")
	names := map[string]bool{}
	for _, p := range merged {
		names[resolveRef(p).GetName()] = true
	}
	assert.True(t, names["id"])
	assert.True(t, names["trace"])
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
	raw, ok := op.Responses[0].Extensions["openapi:links"]
	require.True(t, ok, "response links preserved raw for later promotion")
	assert.Contains(t, string(raw), "GetUserByUserId")
}

func TestGrouping_ByPathPrefixInferred(t *testing.T) {
	t.Parallel()
	spec := pathsSpec(`  /users/{id}:
    get: {operationId: getUser, responses: {"200": {description: ok}}}
  /orders:
    get: {operationId: listOrders, responses: {"200": {description: ok}}}
`)
	opts := Options{Grouping: GroupByPathPrefix}.withDefaults()
	loadedDoc, loadDiags, err := load(t.Context(), 0, compilers.Source{Path: "spec.yaml", Data: []byte(spec)}, opts)
	require.NoError(t, err)
	require.NotNil(t, loadedDoc)
	l := newLowerer(0, loadedDoc, opts)
	l.lowerComponentSchemas()
	svc := l.lowerService()
	requireNoErrorDiags(t, append(loadDiags, l.diags...))
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
	assert.Equal(t, canonicalWords("get /ping"), op.Name.Hint)
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
	assert.NotEmpty(t, getA.Extensions, "op x-* extension")
	assert.NotEmpty(t, getA.Docs.ExternalDocs, "op externalDocs")
	_, hasServers := getA.Extensions["openapi:servers"]
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
	loadedDoc, _, err := load(t.Context(), 0, sourceOf(spec), opts)
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
	assert.Nil(t, ec.Extensions, "headers with no raw node are not preserved")
	require.Empty(t, l.diags)
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
	l.applyPathServers(op, &soa.PathItem{Servers: []*soa.Server{{URL: "https://x"}}})
	assert.Nil(t, op.Extensions, "servers with no raw node are not preserved")
	assert.Empty(t, l.diags)
}

func TestLowerTagDefs_NilEntrySkipped(t *testing.T) {
	t.Parallel()
	l := newRawLowerer(&soa.OpenAPI{Tags: []*soa.Tag{nil, {}}})
	l.lowerTagDefs()
	assert.Len(t, l.out.TagDefs, 1, "nil tag entry skipped")
}

func TestRawChildNode(t *testing.T) {
	t.Parallel()
	assert.Nil(t, rawChildNode(nil, "x"), "nil root")
	assert.Nil(t, rawChildNode(scalarNode("!!str", "x"), "k"), "non-mapping root")

	var doc yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte("a: 1\nb: 2"), &doc))
	// doc is a DocumentNode wrapping the mapping — exercises the unwrap branch.
	got := rawChildNode(&doc, "b")
	require.NotNil(t, got)
	assert.Equal(t, "2", got.Value)
	assert.Nil(t, rawChildNode(&doc, "missing"), "absent key")
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
	assert.Nil(t, resolveRef(rpi))
	assert.Nil(t, resolveRef(rr))
	assert.Nil(t, resolveRef(rh))
	assert.Nil(t, resolveRef(rcb))
	assert.Nil(t, resolveRef(rp))
	assert.Nil(t, resolveRef(rrb))
	assert.Nil(t, resolveRef(re))
	assert.Nil(t, resolveRef(rss))
	_, ok := paramKey(nil)
	assert.False(t, ok)
}
