package openapi

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
	spec := `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /:
    get: {operationId: root, responses: {"200": {description: ok}}}
`
	opts := Options{Grouping: GroupByPathPrefix}.withDefaults()
	loadedDoc, _, err := load(t.Context(), 0, sourceOf(spec), opts)
	require.NoError(t, err)
	l := newLowerer(0, loadedDoc, opts)
	l.lowerComponentSchemas()
	svc := l.lowerService()
	require.NotEmpty(t, svc.Groups)
	assert.Equal(t, "", svc.Groups[0].Name.Source, "root path yields empty first segment")
}
