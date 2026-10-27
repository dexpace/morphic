package operation_test

import (
	"encoding/json/jsontext"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

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
	openapitest.RequireNoErrorDiags(t, diags)
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
	openapitest.RequireNoErrorDiags(t, diags)

	// kept is where the preserved list is recorded (the servers keyword);
	// reported is where the diagnostic is stamped (the operation itself).
	for _, tc := range []struct{ op, url, kept, reported string }{
		{"postP", "https://path.example", "/paths/~1p/servers", "/paths/~1p/post"},
		{"onHook", "https://webhook.example", "/webhooks/hooked/servers", "/webhooks/hooked/post"},
		{"onEvent", "https://callback.example",
			"/paths/~1p/post/callbacks/onEvent/{$request.body#~1url}/servers",
			"/paths/~1p/post/callbacks/onEvent/{$request.body#~1url}/post"},
	} {
		entry, ok := openapitest.FindOp(t, doc, tc.op).Unmodeled["openapi:servers"]
		require.True(t, ok, "%s keeps its path item's servers", tc.op)
		assert.Equal(t, ir.ReasonNoIRHome, entry.Reason)
		assert.JSONEq(t, `[{"url":"`+tc.url+`"}]`, string(entry.Value),
			"%s keeps the list its own path item declared", tc.op)
		assert.Equal(t, jsontext.Pointer(tc.kept), entry.Provenance.Pointer)
		assert.True(t, openapitest.HasDiagCodeAt(diags, diag.DegradedConstruct, tc.reported),
			"%s reports the degradation as the paths route already did", tc.op)
	}
}

// TestPathItem_KeptFindingsAreNotInferred pins that the findings reporting a
// path item's servers and documentation, kept on each operation it mounts, name
// no heuristic. Path-prefix grouping marks the operation, and both findings used
// to take that marker from the operation's provenance although the path item
// declares what they report.
func TestPathItem_KeptFindingsAreNotInferred(t *testing.T) {
	t.Parallel()
	svc, diags := serviceWithGrouping(t, openapitest.PathsSpec(`  /x:
    summary: path summary
    servers: [{url: 'https://path.example'}]
    get:
      operationId: getX
      responses: {"200": {description: ok}}
`), lowering.GroupByPathPrefix)
	openapitest.RequireNoErrorDiags(t, diags)
	require.Len(t, svc.Groups, 1)
	require.Len(t, svc.Groups[0].Operations, 1)
	op := svc.Groups[0].Operations[0]
	require.Equal(t, ir.Inferred("group-path-prefix"), op.Provenance.Inferred,
		"the operation is marked, so the findings had a marker to take")

	for _, prefix := range []string{"path-item servers kept", "path-item documentation kept"} {
		var found []ir.Diagnostic
		for _, d := range diags {
			if d.Code == diag.DegradedConstruct && strings.HasPrefix(d.Message, prefix) {
				found = append(found, d)
			}
		}
		require.Len(t, found, 1, prefix)
		assert.Empty(t, found[0].Provenance.Inferred, "%s: the path item declares it", prefix)
		assert.Equal(t, op.Provenance.Pointer, found[0].Provenance.Pointer, "%s: reported at the operation", prefix)
	}
}

// pathItemUnknownKeySpec writes one key the Path Item Object does not define on
// each of the three path items a document can declare, valued with its route so
// an entry recovered from the wrong item cannot pass for the right one.
//
// Each value is an operation, because the library folds an unrecognized key
// into the item's operations map as one. Each item also writes a second such
// key whose value carries a YAML anchor, which must be kept and reported as the
// plain one is, on every route (GitHub #412, #459).
const pathItemUnknownKeySpec = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /p:
    onPath: {responses: {"200": {description: PATH}}}
    onPathAnchored: &pa {responses: {"200": {description: PATHANCHORED}}}
    post:
      operationId: postP
      callbacks:
        onEvent:
          '{$request.body#/url}':
            onCallback: {responses: {"200": {description: CALLBACK}}}
            onCallbackAnchored: &ca {responses: {"200": {description: CALLBACKANCHORED}}}
            post:
              operationId: onEvent
              responses: {"200": {description: ok}}
      responses: {"200": {description: ok}}
webhooks:
  hooked:
    onWebhook: {responses: {"200": {description: WEBHOOK}}}
    onWebhookAnchored: &wa {responses: {"200": {description: WEBHOOKANCHORED}}}
    post:
      operationId: onHook
      responses: {"200": {description: ok}}
`

// TestOperations_PathItemUnknownKeyKeptOnEveryRoute holds the path item's census
// to all three routes that lower one, for the reason recorded above the servers
// case beside it: the same object has three parents, and that half of
// applyPathItem was live on the paths walk and missing on the other two
// (GitHub #39). Both halves now run from the one entry point, and this is what
// says so rather than assuming it.
func TestOperations_PathItemUnknownKeyKeptOnEveryRoute(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, pathItemUnknownKeySpec)
	openapitest.RequireNoErrorDiags(t, diags)

	for _, tc := range []struct{ op, key, marker, at string }{
		{"postP", "openapi:pathItem/onPath", "PATH", "/paths/~1p/onPath"},
		{"postP", "openapi:pathItem/onPathAnchored", "PATHANCHORED", "/paths/~1p/onPathAnchored"},
		{"onHook", "openapi:pathItem/onWebhook", "WEBHOOK", "/webhooks/hooked/onWebhook"},
		{"onHook", "openapi:pathItem/onWebhookAnchored", "WEBHOOKANCHORED",
			"/webhooks/hooked/onWebhookAnchored"},
		{"onEvent", "openapi:pathItem/onCallback", "CALLBACK",
			"/paths/~1p/post/callbacks/onEvent/{$request.body#~1url}/onCallback"},
		{"onEvent", "openapi:pathItem/onCallbackAnchored", "CALLBACKANCHORED",
			"/paths/~1p/post/callbacks/onEvent/{$request.body#~1url}/onCallbackAnchored"},
	} {
		entry, ok := openapitest.FindOp(t, doc, tc.op).Unmodeled[tc.key]
		require.True(t, ok, "%s keeps the key its own path item wrote", tc.op)
		assert.Equal(t, ir.ReasonOutOfScope, entry.Reason)
		assert.JSONEq(t, `{"responses":{"200":{"description":"`+tc.marker+`"}}}`, string(entry.Value),
			"%s keeps the value its own path item declared", tc.op)
		assert.Equal(t, jsontext.Pointer(tc.at), entry.Provenance.Pointer)
		assert.True(t, openapitest.HasDiagCodeAt(diags, diag.UnknownObjectKey, tc.at),
			"%s announces the key at the key's own pointer", tc.op)
	}
}

// pathItemDocsSpec declares summary and description on each of the three path
// items a document can hold — a path, a webhook, and a callback expression —
// with distinct text apiece so a preserved pair can be traced to the path item
// that wrote it. One operation declares its own summary beside them, which is
// what shows that the path item's pair does not displace it.
const pathItemDocsSpec = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /p:
    summary: path summary
    description: path description
    post:
      operationId: postP
      summary: operation summary
      callbacks:
        onEvent:
          '{$request.body#/url}':
            summary: callback summary
            description: callback description
            post:
              operationId: onEvent
              responses: {"200": {description: ok}}
      responses: {"200": {description: ok}}
webhooks:
  hooked:
    summary: webhook summary
    description: webhook description
    post:
      operationId: onHook
      responses: {"200": {description: ok}}
`

// TestPathItem_DocsKeptOnEveryRoute pins that a path item's summary and
// description reach the IR at all, on whichever of the three routes reaches the
// path item.
//
// Both were read nowhere: fillOperationDocs reads the Operation Object's own
// pair, so a path item's documentation reached neither Docs, nor Unmodeled, nor
// a diagnostic. Each route is asserted with its own text, so a fix that kept the
// enclosing path item's pair — or the same one three times — fails rather than
// passing on the shape alone.
func TestPathItem_DocsKeptOnEveryRoute(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, pathItemDocsSpec)
	openapitest.RequireNoErrorDiags(t, diags)

	// kept is the path item's own pointer, where the pair is declared;
	// reported is the operation, which is what carries the entry.
	for _, tc := range []struct{ op, text, kept, reported string }{
		{"postP", "path", "/paths/~1p", "/paths/~1p/post"},
		{"onHook", "webhook", "/webhooks/hooked", "/webhooks/hooked/post"},
		{"onEvent", "callback",
			"/paths/~1p/post/callbacks/onEvent/{$request.body#~1url}",
			"/paths/~1p/post/callbacks/onEvent/{$request.body#~1url}/post"},
	} {
		op := openapitest.FindOp(t, doc, tc.op)
		for _, field := range []struct{ key, keyword string }{
			{"openapi:pathItemSummary", "summary"},
			{"openapi:pathItemDescription", "description"},
		} {
			entry, ok := op.Unmodeled[field.key]
			require.True(t, ok, "%s keeps its path item's %s", tc.op, field.keyword)
			assert.Equal(t, ir.ReasonNoIRHome, entry.Reason)
			assert.JSONEq(t, `"`+tc.text+` `+field.keyword+`"`, string(entry.Value),
				"%s keeps the text its own path item declared", tc.op)
			assert.Equal(t, jsontext.Pointer(tc.kept+"/"+field.keyword), entry.Provenance.Pointer)
		}
		assert.True(t, openapitest.HasDiagCodeAt(diags, diag.DegradedConstruct, tc.reported),
			"%s reports the path item's documentation as kept rather than lowered", tc.op)
	}

	assert.Equal(t, "operation summary", openapitest.FindOp(t, doc, "postP").Docs.Summary,
		"an operation's own summary is what Docs holds; the path item's never displaces it")
	assert.Empty(t, openapitest.FindOp(t, doc, "onHook").Docs.Summary,
		"and an operation that declares none gets none invented for it")
}

// TestPathItem_DocsAbsentKeepNothing pins the other half: a path item that
// documents nothing writes no entry and announces nothing, so the preservation
// is not a per-operation constant.
func TestPathItem_DocsAbsentKeepNothing(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.PathsSpec(`  /p:
    get: {operationId: getP, responses: {"200": {description: ok}}}
`))
	openapitest.RequireNoErrorDiags(t, diags)

	op := openapitest.FindOp(t, doc, "getP")
	assert.NotContains(t, op.Unmodeled, "openapi:pathItemSummary")
	assert.NotContains(t, op.Unmodeled, "openapi:pathItemDescription")
	assert.False(t, openapitest.HasDiagCodeAt(diags, diag.DegradedConstruct, "/paths/~1p/get"))
}

// pathItemOperationsSpec declares a 3.2 operation with no fixed field of its own
// at each of the three path-item routes, plus the 3.2 `query` fixed field, with
// a distinct operationId apiece.
const pathItemOperationsSpec = `openapi: 3.2.0
info: {title: T, version: "1"}
paths:
  /p:
    query:
      operationId: queryP
      responses: {"200": {description: ok}}
    post:
      operationId: postP
      callbacks:
        onEvent:
          '{$request.body#/url}':
            additionalOperations:
              PURGE:
                operationId: purgeCallback
                responses: {"204": {description: purged}}
      responses: {"200": {description: ok}}
    additionalOperations:
      PURGE:
        operationId: purgeP
        requestBody:
          content:
            application/json: {schema: {type: object, properties: {n: {type: string}}}}
        responses: {"204": {description: purged}}
      lowercase-purge:
        operationId: purgeVerbatim
        responses: {"204": {description: purged}}
webhooks:
  hooked:
    additionalOperations:
      FLUSH:
        operationId: flushHook
        responses: {"204": {description: flushed}}
`

// TestPathItem_AdditionalOperationsLowerOnEveryRoute pins that an operation
// declared under 3.2 additionalOperations becomes an ir.Operation like any
// other, on whichever of the three routes reaches the path item.
//
// The loop over the fixed method fields was the whole of the walk, so every such
// operation was dropped entire — operationId, parameters, request body,
// responses — with no diagnostic. Each route is asserted separately because a
// fix scoped to the paths walk leaves the other two exactly as they were.
func TestPathItem_AdditionalOperationsLowerOnEveryRoute(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, pathItemOperationsSpec)
	openapitest.RequireNoErrorDiags(t, diags)

	for _, tc := range []struct{ op, method, id string }{
		{"queryP", "QUERY", "op/openapi/paths/~1p/query"},
		{"purgeP", "PURGE", "op/openapi/paths/~1p/additionalOperations/PURGE"},
		{"purgeVerbatim", "lowercase-purge", "op/openapi/paths/~1p/additionalOperations/lowercase-purge"},
		{"flushHook", "FLUSH", "op/openapi/webhooks/hooked/additionalOperations/FLUSH"},
		{"purgeCallback", "PURGE",
			"op/openapi/paths/~1p/post/callbacks/onEvent/{$request.body#~1url}/additionalOperations/PURGE"},
	} {
		op := openapitest.FindOp(t, doc, tc.op)
		assert.Equal(t, ir.OpID(tc.id), op.ID, "%s is identified by where it is written", tc.op)
		require.Len(t, op.Bindings.HTTP, 1)
		assert.Equal(t, tc.method, op.Bindings.HTTP[0].Method,
			"%s binds the method key as the source spelled it", tc.op)
	}

	flushHook := openapitest.FindOp(t, doc, "flushHook")
	assert.True(t, flushHook.Bindings.HTTP[0].IsWebhook,
		"a webhook mount marks the binding whichever field declared the operation")
	assert.Equal(t, "hooked", flushHook.Bindings.HTTP[0].WebhookName,
		"the webhooks-map key names the event")
	assert.Empty(t, flushHook.Bindings.HTTP[0].URITemplate)

	// Nothing reachable only through a dropped operation reached the registry
	// either: the request body's schema was not interned at all.
	purge := openapitest.FindOp(t, doc, "purgeP")
	require.NotNil(t, purge.Request)
	require.Len(t, purge.Request.Contents, 1)
	assert.NotNil(t, doc.Types[purge.Request.Contents[0].Type.Target],
		"the request body schema is interned, not merely referenced")
}

// TestPathItem_AdditionalOperationsBindCallbacks pins that the parent operation
// records a callback lowered from additionalOperations alone, so an expression
// whose path item declares no fixed method is still bound rather than left
// holding an empty operation list.
func TestPathItem_AdditionalOperationsBindCallbacks(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, pathItemOperationsSpec)
	openapitest.RequireNoErrorDiags(t, diags)

	parent := openapitest.FindOp(t, doc, "postP")
	require.Len(t, parent.Bindings.HTTP, 1)
	require.Len(t, parent.Bindings.HTTP[0].Callbacks, 1)
	assert.Equal(t, []ir.OpID{openapitest.FindOp(t, doc, "purgeCallback").ID},
		parent.Bindings.HTTP[0].Callbacks[0].Operations)
}

// TestPathItem_EmptyAdditionalOperationsKeyReported pins the one key that names
// no method. Taking the key verbatim is what makes it reachable: an empty one
// binds an empty HTTP method, which no request can be sent with.
//
// The entry still lowers, so nothing it declares is lost — dropping it would
// trade a reported defect for a silent one, which is the trade this whole change
// exists to undo. speakeasy rejects a key naming a standard method and accepts
// this one, so the compiler is the only thing that can report it.
func TestPathItem_EmptyAdditionalOperationsKeyReported(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, `openapi: 3.2.0
info: {title: T, version: "1"}
paths:
  /p:
    additionalOperations:
      "":
        operationId: nameless
        responses: {"204": {description: done}}
`)
	openapitest.RequireNoErrorDiags(t, diags)

	op := openapitest.FindOp(t, doc, "nameless")
	require.Len(t, op.Bindings.HTTP, 1)
	assert.Empty(t, op.Bindings.HTTP[0].Method, "the key binds as written, empty or not")
	assert.Equal(t, ir.OpID("op/openapi/paths/~1p/additionalOperations/"), op.ID,
		"and the operation is still mounted where the source writes it")

	assert.True(t, openapitest.HasDiagCodeAt(diags, diag.InvalidMethodKey, "/paths/~1p/additionalOperations/"),
		"the unusable method is reported at the entry that declares it")
	for _, d := range diags {
		if d.Code == diag.InvalidMethodKey {
			assert.Equal(t, ir.SeverityWarning, d.Severity,
				"a warning: the operation lowers in full, only its method is unusable")
		}
	}
}

// TestPathItem_WithNoOperationKeepsWhatItWroteOnTheService covers the one path
// item shape that reached applyPathItem through nothing.
//
// applyPathItem runs once per operation an item produces, so an item producing
// none lost its servers, its extensions and its undeclared keys whole, with no
// diagnostic naming the loss. The service is where they go now — the same node
// the Paths Object's own extensions go to, and for the same reason: a path item
// lowers to no node of its own.
func TestPathItem_WithNoOperationKeepsWhatItWroteOnTheService(t *testing.T) {
	t.Parallel()
	svc, diags := serviceWithGrouping(t, `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /mounted:
    get: {operationId: getX, responses: {"200": {description: ok}}}
  /unmounted:
    servers: [{url: 'https://a.example'}]
    x-kept: {a: 1}
webhooks:
  hook:
    x-hook-kept: {b: 2}
`, lowering.GroupByTags)

	require.Contains(t, svc.Unmodeled, "openapi:pathItem/paths/~1unmounted/x-kept")
	require.Contains(t, svc.Unmodeled, "openapi:pathItem/paths/~1unmounted/servers")
	require.Contains(t, svc.Unmodeled, "openapi:pathItem/webhooks/hook/x-hook-kept")
	assert.Equal(t, ir.ReasonNoIRHome, svc.Unmodeled["openapi:pathItem/paths/~1unmounted/servers"].Reason)

	// The key carries the item's own pointer because one service holds every such
	// item: two of them under a bare prefix would collide, and the survivor would
	// be whichever the walk reached last.
	assert.NotContains(t, svc.Unmodeled, "openapi:servers",
		"the unqualified servers key is the one an operation uses")

	var announced int
	for _, d := range diags {
		if strings.Contains(d.Message, "declares no operation this compiler lowers") {
			announced++
		}
	}
	assert.Equal(t, 2, announced, "one per unmounted item: the path and the webhook")
}

// TestPathItem_UnmountedItemsAreAnnouncedApart holds the announcement to one per
// item rather than one per document.
//
// Both notices are written by the same call at the same severity with the same
// text, so the item's own pointer is the only thing telling them apart — and
// diagnostic identity is the whole value, so a carrier stamping the node's
// pointer instead of the item's collapsed every unmounted item in a document
// into a single finding that named none of them. Two items are the smallest
// input that can see it; one passes either way.
func TestPathItem_UnmountedItemsAreAnnouncedApart(t *testing.T) {
	t.Parallel()
	_, diags := serviceWithGrouping(t, `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a: {servers: [{url: 'https://a.example'}]}
  /b: {servers: [{url: 'https://b.example'}]}
`, lowering.GroupByTags)

	var at []string
	for _, d := range diags {
		if strings.Contains(d.Message, "path-item servers kept under Unmodeled") {
			at = append(at, string(d.Provenance.Pointer))
		}
	}
	assert.Equal(t, []string{"/paths/~1a", "/paths/~1b"}, at,
		"each kept list is announced at the item that wrote it")
}

// TestPathItem_UnmountedAnnouncementFollowsWhatWasKept pins the announcement to
// what landed rather than to what the source model reports.
//
// Servers merged in through `<<` are read as written ones are, kept and
// announced (GitHub #384). Merged in from past nodeview.MergeDepthLimit, the
// model reads them and the compiler's raw read does not, so a predicate over
// the model would say they were "kept beside it" while nothing was kept: a
// diagnostic asserting a preservation that did not happen.
func TestPathItem_UnmountedAnnouncementFollowsWhatWasKept(t *testing.T) {
	t.Parallel()
	const servers = "{servers: [{url: 'https://m.example'}]}"
	for _, tc := range []struct {
		name, item string
		kept       bool
	}{
		{"merged in", "{<<: " + servers + "}", true},
		{"merged in past the bound", openapitest.NestedMerges(nodeview.MergeDepthLimit+1, servers), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			svc, diags := serviceWithGrouping(t, "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\n"+
				"paths:\n  /unmounted: "+tc.item+"\n", lowering.GroupByTags)

			assert.Equal(t, tc.kept, len(svc.Unmodeled) == 1, "%v", svc.Unmodeled)
			announced := slices.ContainsFunc(diags, func(d ir.Diagnostic) bool {
				return strings.Contains(d.Message, "declares no operation this compiler lowers")
			})
			assert.Equal(t, tc.kept, announced, "the announcement follows what landed")
		})
	}
}

// TestPathItem_CallbackWithNoOperationKeepsWhatItWrote is the third route's
// half of the orphan branch: a callback expression is a Path Item Object like
// any other, so an item that mounts no operation must still keep what it wrote,
// as on the paths and webhooks walks (GitHub #39). The entry point is shared,
// and this asserts the third route rather than assuming it.
//
// The parent's HTTP binding is the carrier: a callback lowers to no node holding
// an Unmodeled map, and the binding is where the Callback Object's own
// extensions already go.
func TestPathItem_CallbackWithNoOperationKeepsWhatItWrote(t *testing.T) {
	t.Parallel()
	svc, diags := serviceWithGrouping(t, `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /p:
    post:
      operationId: postP
      responses: {"200": {description: ok}}
      callbacks:
        onEvent:
          '{$request.body#/url}':
            servers: [{url: 'https://cb.example'}]
            x-cb-kept: {a: 1}
`, lowering.GroupByTags)

	op := openapitest.FirstOp(t, svc)
	require.Len(t, op.Bindings.HTTP, 1)
	kept := op.Bindings.HTTP[0].Unmodeled
	base := "openapi:pathItem/paths/~1p/post/callbacks/onEvent/{$request.body#~1url}"
	require.Contains(t, kept, base+"/servers")
	require.Contains(t, kept, base+"/x-cb-kept")

	var announced int
	for _, d := range diags {
		if strings.Contains(d.Message, "declares no operation this compiler lowers") {
			announced++
			assert.Equal(t, jsontext.Pointer("/paths/~1p/post/callbacks/onEvent/{$request.body#~1url}"),
				d.Provenance.Pointer, "announced at the expression whose item mounts nothing")
		}
	}
	assert.Equal(t, 1, announced)
}

// TestPathItem_WithNoOperationAndNothingToKeepIsSilent holds the other half: an
// item that produces no operation and wrote nothing beside it has lost nothing,
// so there is nothing to keep and nothing to announce.
func TestPathItem_WithNoOperationAndNothingToKeepIsSilent(t *testing.T) {
	t.Parallel()
	svc, diags := serviceWithGrouping(t, `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /empty: {}
  /described: {summary: s, description: d}
`, lowering.GroupByTags)

	assert.Empty(t, svc.Unmodeled)
	for _, d := range diags {
		assert.NotContains(t, d.Message, "declares no operation this compiler lowers",
			"an item with nothing beside its operations announces nothing")
	}
}
