// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package operation_test

import (
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/operation"
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
	assert.Equal(t, ir.HTTPInboundWebhook, op.Bindings.HTTP[0].Inbound)
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

	callbacks := openapitest.FindOp(t, doc, "postP").Bindings.HTTP[0].Callbacks
	names := make([]string, 0, len(callbacks))
	for _, cb := range callbacks {
		names = append(names, cb.Name)
		cbOp := openapitest.FindOp(t, doc, map[string]string{"a": "cbA", "a/x-b": "cbB"}[cb.Name])
		assert.Equal(t, []ir.OpID{cbOp.ID}, cb.Operations, "callback %q lists its own operation", cb.Name)
		assert.Equal(t, ir.HTTPInboundCallback, cbOp.Bindings.HTTP[0].Inbound, "%s is marked a callback", cbOp.ID)
	}
	assert.ElementsMatch(t, []string{"a", "a/x-b"}, names, "each callback keeps the name it is mapped by, verbatim")
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
	assert.Equal(t, ir.HTTPInboundWebhook, ops[idA].Bindings.HTTP[0].Inbound)
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

// nestedCallbacks returns a path whose operation declares a callback whose
// operation declares one, depth levels deep, each operation named cb<level>
// and its expression {$request.body#/u<level>}.
func nestedCallbacks(depth int) string {
	var b strings.Builder
	b.WriteString("openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\ntags: [{name: root}]\npaths:\n  /a:\n    post:\n" +
		"      operationId: cb0\n      tags: [root]\n      responses: {\"200\": {description: ok}}\n")
	indent := "      "
	for level := 1; level <= depth; level++ {
		fmt.Fprintf(&b, "%scallbacks:\n%s  c%d:\n%s    '{$request.body#/u%d}':\n%s      post:\n",
			indent, indent, level, indent, level, indent)
		indent += "        "
		fmt.Fprintf(&b, "%soperationId: cb%d\n%stags: [other]\n%sresponses: {\"200\": {description: ok}}\n",
			indent, level, indent, indent)
	}
	return b.String()
}

// TestCallbacks_DeclaredOnACallbackLower pins that a callback operation's own
// callbacks lower as any operation's do, down to MaxCallbackNesting levels: an
// Operation each, marked a callback, listed by the binding of the operation
// declaring it, inheriting no security and filed with the path's operation,
// whatever its tags. The callbacks one level deeper than the bound are kept
// verbatim on the deepest operation's binding and reported there; a document
// nested to the bound reports nothing.
func TestCallbacks_DeclaredOnACallbackLower(t *testing.T) {
	t.Parallel()
	for _, depth := range []int{operation.MaxCallbackNesting, operation.MaxCallbackNesting + 1} {
		t.Run(strconv.Itoa(depth), func(t *testing.T) {
			t.Parallel()
			_, svc, diags := compileService(t, nestedCallbacks(depth))
			openapitest.RequireNoErrorDiags(t, diags)

			require.Len(t, svc.Groups, 1, "every callback goes with the path's operation")
			ops := svc.Groups[0].Operations
			require.Len(t, ops, operation.MaxCallbackNesting+1)
			for level, op := range ops {
				hb := op.Bindings.HTTP[0]
				assert.Equal(t, "cb"+strconv.Itoa(level), op.Name.Source, "the declaring operation comes first")
				if level == 0 {
					continue
				}
				assert.Equal(t, ir.HTTPInboundCallback, hb.Inbound)
				assert.Nil(t, op.Auth, "a callback inherits no security")
				parent := ops[level-1].Bindings.HTTP[0].Callbacks
				require.Len(t, parent, 1, "cb%d lists the callback it declares", level-1)
				assert.Equal(t, []ir.OpID{op.ID}, parent[0].Operations)
			}
			deepest := ops[operation.MaxCallbackNesting]
			assert.Empty(t, deepest.Bindings.HTTP[0].Callbacks)
			kept, cut := deepest.Bindings.HTTP[0].Unmodeled["openapi:callbacks"]
			reported := openapitest.HasDiag(diags, diag.DegradedConstruct)
			assert.Equal(t, depth > operation.MaxCallbackNesting, cut, "only callbacks past the bound are kept")
			assert.Equal(t, cut, reported, "and reported")
			if !cut {
				return
			}
			assert.Equal(t, ir.ReasonDegradedLowering, kept.Reason)
			assert.Contains(t, string(kept.Value), "cb"+strconv.Itoa(depth), "the map is kept whole")
			at := string(deepest.Provenance.Pointer) + "/callbacks"
			assert.Equal(t, at, string(kept.Provenance.Pointer), "kept where it is written")
			msg := openapitest.DiagMessageAt(t, diags, diag.DegradedConstruct, ir.SeverityWarning, at)
			assert.Contains(t, msg, "deeper than "+strconv.Itoa(operation.MaxCallbackNesting)+" levels")
		})
	}
}

// TestCallbacks_ACallbackNamingItselfStopsAtTheBound pins the input the bound
// is for: a callback whose operation declares the callback again by $ref would
// lower without end, and instead lowers MaxCallbackNesting levels.
func TestCallbacks_ACallbackNamingItselfStopsAtTheBound(t *testing.T) {
	t.Parallel()
	_, svc, diags := compileService(t, `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /ping:
    post:
      operationId: ping
      responses: {"200": {description: ok}}
      callbacks:
        pong: {$ref: '#/components/callbacks/Pong'}
components:
  callbacks:
    Pong:
      '{$request.body#/url}':
        post:
          responses: {"200": {description: ok}}
          callbacks:
            again: {$ref: '#/components/callbacks/Pong'}
`)
	openapitest.RequireNoErrorDiags(t, diags)
	require.Len(t, svc.Groups, 1)
	assert.Len(t, svc.Groups[0].Operations, operation.MaxCallbackNesting+1)
	assert.True(t, openapitest.HasDiagCodeAt(diags, diag.DegradedConstruct,
		"/components/callbacks/Pong/{$request.body#~1url}/post/callbacks"))
}
