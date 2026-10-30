// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package operation_test

import (
	"encoding/json/jsontext"
	"slices"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

func TestContent_NonRequiredRequestBody(t *testing.T) {
	t.Parallel()
	spec := openapitest.PathsSpec(`  /maybe:
    post:
      operationId: maybe
      requestBody:
        content:
          application/json: {schema: {type: object, properties: {n: {type: string}}}}
      responses: {"200": {description: ok}}
`)
	_, svc, diags := compileService(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	op := openapitest.FirstOp(t, svc)
	require.NotNil(t, op.Request, "a non-required body still lowers to a present Payload")
	require.NotNil(t, op.Request.Required, "OpenAPI always states body optionality")
	assert.False(t, *op.Request.Required)
	assert.NotContains(t, op.Request.Unmodeled, "openapi:required",
		"the typed field carries the fact, so no sentinel is written beside it")
	for _, d := range diags {
		assert.NotContains(t, d.Message, "request body",
			"a typed fact is not a degraded construct")
	}
}

// TestContent_RequiredRequestBody is TestContent_NonRequiredRequestBody's other
// arm: `required: true` must reach the same field rather than being encoded as
// the sentinel's absence, which is what made a consumer read every body alike.
func TestContent_RequiredRequestBody(t *testing.T) {
	t.Parallel()
	spec := openapitest.PathsSpec(`  /must:
    post:
      operationId: must
      requestBody:
        required: true
        content:
          application/json: {schema: {type: object, properties: {n: {type: string}}}}
      responses: {"200": {description: ok}}
`)
	_, svc, diags := compileService(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	op := openapitest.FirstOp(t, svc)
	require.NotNil(t, op.Request)
	require.NotNil(t, op.Request.Required)
	assert.True(t, *op.Request.Required)
}

// TestContent_ResponsePayloadStatesNoOptionality pins the third state: only a
// request body can be omitted, so a response Payload leaves Required nil and a
// consumer reading it as "false" would be inventing a fact.
func TestContent_ResponsePayloadStatesNoOptionality(t *testing.T) {
	t.Parallel()
	spec := openapitest.PathsSpec(`  /get:
    get:
      operationId: getThing
      responses:
        "200":
          description: ok
          content:
            application/json: {schema: {type: object, properties: {n: {type: string}}}}
`)
	_, svc, diags := compileService(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	op := openapitest.FirstOp(t, svc)
	require.Len(t, op.Responses, 1)
	require.NotNil(t, op.Responses[0].Payload)
	assert.Nil(t, op.Responses[0].Payload.Required,
		"a response body has no optionality to state")
}

// TestContent_RequestBodyDocsReachThePayload pins GitHub #609: a Request Body
// Object's description documents the body, so it reaches Payload.Docs.
func TestContent_RequestBodyDocsReachThePayload(t *testing.T) {
	t.Parallel()
	spec := openapitest.PathsSpec(`  /bodies:
    post:
      operationId: bodies
      requestBody:
        description: The thing to create.
        content:
          application/json: {schema: {type: object, properties: {n: {type: string}}}}
      responses: {"200": {description: ok}}
`)
	_, svc, diags := compileService(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	op := openapitest.FirstOp(t, svc)
	require.NotNil(t, op.Request)
	assert.Equal(t, "The thing to create.", op.Request.Docs.Description,
		"a declared body description has a home on the payload")
}

// TestContent_ResponsePayloadStatesNoDocs pins the other side: a response's
// description documents the response, so its payload is given none.
func TestContent_ResponsePayloadStatesNoDocs(t *testing.T) {
	t.Parallel()
	spec := openapitest.PathsSpec(`  /read:
    get:
      operationId: read
      responses:
        "200":
          description: ok
          content:
            application/json: {schema: {type: object, properties: {n: {type: string}}}}
`)
	_, svc, diags := compileService(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	op := openapitest.FirstOp(t, svc)
	require.Len(t, op.Responses, 1)
	require.NotNil(t, op.Responses[0].Payload)
	assert.Zero(t, op.Responses[0].Payload.Docs, "only the request-body lowering writes payload docs")
	assert.Equal(t, "ok", op.Responses[0].Docs.Description)
}

// TestContent_RequestBodyDocsAreOrderIndependent is the hand-rolled two-order
// diff for GitHub #609. The order-invariance oracle compares only the type
// registry and the diagnostic set, and Payload.Docs is neither, so the swap is
// made here: one components/requestBodies entry referenced by two operations,
// declared in both orders. The docs are read from the component whichever mount
// arrives first, and that shared body interns once at the component pointer, so
// neither fact may move with the declaration order.
func TestContent_RequestBodyDocsAreOrderIndependent(t *testing.T) {
	t.Parallel()
	const createOrder = `  /orders:
    post:
      operationId: createOrder
      requestBody: {$ref: '#/components/requestBodies/OrderBody'}
      responses: {"200": {description: ok}}
`
	const replaceOrder = `  /orders/{id}:
    put:
      operationId: replaceOrder
      parameters:
        - {name: id, in: path, required: true, schema: {type: string}}
      requestBody: {$ref: '#/components/requestBodies/OrderBody'}
      responses: {"200": {description: ok}}
`
	projection := func(paths string) map[string]string {
		t.Helper()
		spec := `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
` + paths + `components:
  requestBodies:
    OrderBody:
      description: A shared order body.
      required: true
      content:
        application/json:
          schema: {type: object, properties: {sku: {type: string}}}
`
		doc, _, diags := compileService(t, spec)
		openapitest.RequireNoErrorDiags(t, diags)
		out := map[string]string{}
		for _, name := range []string{"createOrder", "replaceOrder"} {
			op := openapitest.FindOp(t, doc, name)
			require.NotNil(t, op.Request, "%s: the body lowers", name)
			out[name] = op.Request.Docs.Description + " " + string(openapitest.BodyTarget(t, op.Request))
		}
		return out
	}
	assert.Empty(t, cmp.Diff(projection(createOrder+replaceOrder), projection(replaceOrder+createOrder)),
		"the shared body's docs and identity must not depend on which mount lowered first")
}

// TestLowerRequestBody_AnEmptyContentKeepsTheBody pins a request body whose
// content map is empty (GitHub #622): it lowers to a Payload naming no media
// type, keeping whether it must be sent, its description and its x-*, and is
// reported once, at the map of the declaration every use shares. A content
// left out keeps the body too, and is the parser's to report.
func TestLowerRequestBody_AnEmptyContentKeepsTheBody(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, `openapi: 3.2.0
info: {title: T, version: "1"}
paths:
  /inline:
    post:
      operationId: inline
      requestBody: {description: the body, required: true, x-note: kept, content: {}}
      responses: {"204": {description: none}}
  /shared:
    post:
      operationId: sharedPost
      requestBody: {$ref: '#/components/requestBodies/Empty'}
      responses: {"204": {description: none}}
    put:
      operationId: sharedPut
      requestBody: {$ref: '#/components/requestBodies/Empty'}
      responses: {"204": {description: none}}
  /missing:
    post:
      operationId: missing
      requestBody: {description: no content field}
      responses: {"204": {description: none}}
  /typed:
    post:
      operationId: typed
      requestBody: {content: {text/plain: {schema: {type: string}}}}
      responses: {"204": {description: none}}
components:
  requestBodies:
    Empty: {content: {}}
`)
	inline := openapitest.FindOp(t, doc, "inline")
	require.NotNil(t, inline.Request)
	assert.Empty(t, inline.Request.Contents)
	assert.Equal(t, new(true), inline.Request.Required)
	assert.Equal(t, "the body", inline.Request.Docs.Description)
	assert.Contains(t, inline.Request.Unmodeled, "openapi:x-note")
	assert.Empty(t, inline.Bindings.HTTP[0].RequestContentTypes)
	for _, name := range []string{"sharedPost", "sharedPut", "missing"} {
		op := openapitest.FindOp(t, doc, name)
		require.NotNil(t, op.Request, name)
		assert.Empty(t, op.Request.Contents, name)
		assert.Equal(t, new(false), op.Request.Required, name)
	}

	var at []jsontext.Pointer
	for _, d := range diags {
		if d.Code == diag.EmptyRequestContent {
			assert.Equal(t, ir.SeverityWarning, d.Severity)
			at = append(at, d.Provenance.Pointer)
		}
	}
	slices.Sort(at)
	assert.Equal(t, []jsontext.Pointer{"/components/requestBodies/Empty/content", "/paths/~1inline/post/requestBody/content"}, at)
	assert.True(t, slices.ContainsFunc(diags, func(d ir.Diagnostic) bool {
		return d.Code == diag.Validation+"/validation-required-field" &&
			d.Provenance.Pointer == "/paths/~1missing/post/requestBody"
	}), "the rule table reports the content left out: %+v", diags)
}

// TestContent_RequestBodyRefSharedAcrossOperationsInternsOnce is the fix's
// core scenario for a referenced requestBody component (issue #107): two
// operations $ref'ing one #/components/requestBodies/Body must intern its
// content schema once, at the component's own declaration pointer.
func TestContent_RequestBodyRefSharedAcrossOperationsInternsOnce(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, componentBodyRefSpec)
	openapitest.RequireNoErrorDiags(t, diags)
	postA := openapitest.FindOp(t, doc, "postA")
	postB := openapitest.FindOp(t, doc, "postB")
	require.NotNil(t, postA.Request)
	require.NotNil(t, postB.Request)

	wantID := ir.TypeID("t/anon/components/requestBodies/Body/content/application~1json/schema")
	assert.Equal(t, wantID, postA.Request.Contents[0].Type.Target)
	assert.Equal(t, wantID, postB.Request.Contents[0].Type.Target,
		"both operations resolve the same shared body schema")

	_, fabricatedA := doc.Types[ir.TypeID("t/anon/paths/~1a/post/requestBody/content/application~1json/schema")]
	_, fabricatedB := doc.Types[ir.TypeID("t/anon/paths/~1b/post/requestBody/content/application~1json/schema")]
	assert.False(t, fabricatedA, "no fabricated per-operation ID for /a")
	assert.False(t, fabricatedB, "no fabricated per-operation ID for /b")
}

// TestContent_RequestBodyRefSiteDocsOverrideTheDeclaration pins the request-body
// half of GitHub #610: a body entry written as a Reference Object keeps the
// description beside its $ref, which wins over the declaration's.
func TestContent_RequestBodyRefSiteDocsOverrideTheDeclaration(t *testing.T) {
	t.Parallel()
	spec := `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a:
    post:
      operationId: a
      requestBody: {$ref: '#/components/requestBodies/Body', description: the use site}
      responses: {"200": {description: ok}}
components:
  requestBodies:
    Body:
      description: declared
      content:
        application/json: {schema: {type: object, properties: {n: {type: string}}}}
`
	_, svc, diags := compileService(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	op := openapitest.FirstOp(t, svc)
	require.NotNil(t, op.Request)
	assert.Equal(t, "the use site", op.Request.Docs.Description)
}
