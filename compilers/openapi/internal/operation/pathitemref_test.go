package operation_test

import (
	"encoding/json/jsontext"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// pathItemRefSiblingsSpec is the issue's own reproducer: a path whose whole
// entry is a $ref to another path, with a summary and an operation written
// beside the reference (GitHub #577).
const pathItemRefSiblingsSpec = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a:
    $ref: '#/paths/~1b'
    summary: written beside the ref
    put:
      operationId: putA
      responses: {"200": {description: OK}}
  /b:
    get:
      operationId: getB
      responses: {"200": {description: OK}}
`

// TestPathItemRefSiblings_IssueReproducer pins the whole fix at the shape the
// issue reported: the operation written beside the $ref reaches the IR at its
// own mount pointer, the sibling's summary is kept, and the referenced item's
// own operation still mounts where the document writes it.
func TestPathItemRefSiblings_IssueReproducer(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, pathItemRefSiblingsSpec)
	openapitest.RequireNoErrorDiags(t, diags)

	putA := openapitest.FindOp(t, doc, "putA")
	assert.Equal(t, ir.OpID("op/openapi/paths/~1a/put"), putA.ID)
	assert.Equal(t, jsontext.Pointer("/paths/~1a/put"), putA.Provenance.Pointer,
		"the sibling operation is declared where it is mounted")
	assert.Equal(t, "PUT", putA.Bindings.HTTP[0].Method)
	assert.Equal(t, "/a", putA.Bindings.HTTP[0].URITemplate)

	entry, ok := putA.Unmodeled["openapi:pathItemSummary"]
	require.True(t, ok, "the summary written beside the $ref is kept")
	assert.JSONEq(t, `"written beside the ref"`, string(entry.Value))
	assert.Equal(t, jsontext.Pointer("/paths/~1a/summary"), entry.Provenance.Pointer)

	// The referent's own operation keeps mounting at both paths: a use site that
	// adds fields beside the $ref does not stop referencing it.
	atA := opByPath(t, doc, "GET", "/a")
	atB := opByPath(t, doc, "GET", "/b")
	assert.Equal(t, ir.OpID("op/openapi/paths/~1a/get"), atA.ID)
	assert.Equal(t, ir.OpID("op/openapi/paths/~1b/get"), atB.ID)
	assert.Equal(t, jsontext.Pointer("/paths/~1b/get"), atA.Provenance.Pointer,
		"the remounted operation is still declared at the component it names")
	assert.Equal(t, "getB", atA.Name.Source)

	// And the sibling's item-level constructs land on every operation mounted
	// at that path, the referent's included.
	assert.Contains(t, atA.Unmodeled, "openapi:pathItemSummary")
	assert.True(t, openapitest.HasDiagCodeAt(diags, diag.DuplicateOperationID, "/paths/~1a/get"),
		"one declaration mounted twice is still the remount warning, unchanged")
	assert.False(t, openapitest.HasDiag(diags, diag.ConflictingOperationID))
}

// pathItemRefWebhookSpec is the same shape under webhooks, which is the second
// route a path item is reached through.
const pathItemRefWebhookSpec = `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
webhooks:
  hooked:
    $ref: '#/components/pathItems/Shared'
    summary: hook summary beside the ref
    post:
      operationId: siblingHook
      responses: {"200": {description: OK}}
components:
  pathItems:
    Shared:
      get:
        operationId: refHook
        responses: {"200": {description: OK}}
`

// TestPathItemRefSiblings_WebhookRoute holds the seam to the webhook route: the
// same read, the same model and the same carriers, reached under webhooks rather
// than paths.
func TestPathItemRefSiblings_WebhookRoute(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, pathItemRefWebhookSpec)
	openapitest.RequireNoErrorDiags(t, diags)

	sibling := openapitest.FindOp(t, doc, "siblingHook")
	assert.Equal(t, ir.OpID("op/openapi/webhooks/hooked/post"), sibling.ID)
	assert.Equal(t, jsontext.Pointer("/webhooks/hooked/post"), sibling.Provenance.Pointer)
	assert.True(t, sibling.Bindings.HTTP[0].IsWebhook, "a webhook operation keeps its binding")
	entry, ok := sibling.Unmodeled["openapi:pathItemSummary"]
	require.True(t, ok, "the webhook's sibling summary is kept")
	assert.Equal(t, jsontext.Pointer("/webhooks/hooked/summary"), entry.Provenance.Pointer)

	referent := openapitest.FindOp(t, doc, "refHook")
	assert.Equal(t, ir.OpID("op/openapi/webhooks/hooked/get"), referent.ID)
	assert.Contains(t, referent.Unmodeled, "openapi:pathItemSummary",
		"the use site's constructs reach the referenced item's operation too")
}

// pathItemRefCallbackSpec is the same shape at a callback expression, the third
// route a path item is reached through.
const pathItemRefCallbackSpec = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /p:
    post:
      operationId: parent
      callbacks:
        onEvent:
          '{$request.body#/url}':
            $ref: '#/components/pathItems/Cb'
            summary: cb summary beside the ref
            post:
              operationId: siblingCb
              responses: {"200": {description: OK}}
      responses: {"200": {description: OK}}
components:
  pathItems:
    Cb:
      get:
        operationId: refCb
        responses: {"200": {description: OK}}
`

// TestPathItemRefSiblings_CallbackRoute holds the seam to the callback route,
// which a document-level walk cannot reach: a $ref'd callback's expressions are
// walked only from the mount that references it.
func TestPathItemRefSiblings_CallbackRoute(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, pathItemRefCallbackSpec)
	openapitest.RequireNoErrorDiags(t, diags)

	sibling := openapitest.FindOp(t, doc, "siblingCb")
	assert.Equal(t, jsontext.Pointer("/paths/~1p/post/callbacks/onEvent/{$request.body#~1url}/post"),
		sibling.Provenance.Pointer)
	entry, ok := sibling.Unmodeled["openapi:pathItemSummary"]
	require.True(t, ok, "the callback expression's sibling summary is kept")
	assert.Equal(t, jsontext.Pointer("/paths/~1p/post/callbacks/onEvent/{$request.body#~1url}/summary"),
		entry.Provenance.Pointer)

	referent := openapitest.FindOp(t, doc, "refCb")
	assert.Equal(t, jsontext.Pointer("/components/pathItems/Cb/get"), referent.Provenance.Pointer)
	assert.Contains(t, referent.Unmodeled, "openapi:pathItemSummary",
		"the use site's constructs reach the referenced item's operation too")
}

// pathItemRefItemSpec declares item-level constructs beside a $ref: servers, an
// x-* extension, an undeclared key and an item-level parameter, none of which
// the reference wrapper models.
const pathItemRefItemSpec = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a:
    $ref: '#/paths/~1b'
    servers: [{url: https://use.example}]
    x-beside: use
    bogusUse: use
    parameters:
      - {name: useParam, in: query, schema: {type: string}}
    put:
      operationId: usePut
      responses: {"200": {description: OK}}
  /b:
    get:
      operationId: getB
      responses: {"200": {description: OK}}
`

// TestPathItemRefSiblings_ItemConstructsReachEveryOperation pins that what the
// use site writes beside the $ref reaches the IR on every operation mounted at
// that path, each under its own pointer, exactly as a path item's own
// constructs do: the referenced item's operation is asserted beside the one
// written next to the reference.
func TestPathItemRefSiblings_ItemConstructsReachEveryOperation(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, pathItemRefItemSpec)
	openapitest.RequireNoErrorDiags(t, diags)

	mounted := []ir.Operation{
		opByPath(t, doc, "GET", "/a"),        // the referent's, mounted at this path
		openapitest.FindOp(t, doc, "usePut"), // the use site's own
	}
	for _, op := range mounted {
		entry, ok := op.Unmodeled["openapi:servers"]
		require.True(t, ok, "%s keeps the use site's servers", op.Name.Source)
		assert.JSONEq(t, `[{"url":"https://use.example"}]`, string(entry.Value))
		assert.Equal(t, jsontext.Pointer("/paths/~1a/servers"), entry.Provenance.Pointer)

		ext, ok := op.Unmodeled["openapi:pathItem/x-beside"]
		require.True(t, ok, "%s keeps the use site's x-*", op.Name.Source)
		assert.JSONEq(t, `"use"`, string(ext.Value))
		assert.Equal(t, jsontext.Pointer("/paths/~1a/x-beside"), ext.Provenance.Pointer)

		unknown, ok := op.Unmodeled["openapi:pathItem/bogusUse"]
		require.True(t, ok, "%s keeps the use site's undeclared key", op.Name.Source)
		assert.JSONEq(t, `"use"`, string(unknown.Value))
		assert.Equal(t, jsontext.Pointer("/paths/~1a/bogusUse"), unknown.Provenance.Pointer)

		require.Len(t, op.Params, 1, "%s merges the use site's item parameter", op.Name.Source)
		assert.Equal(t, "useParam", op.Params[0].Name.Source)
		assert.Equal(t, jsontext.Pointer("/paths/~1a/parameters/0"), op.Params[0].Provenance.Pointer)
	}
	assert.True(t, openapitest.HasDiagCodeAt(diags, diag.UnknownObjectKey, "/paths/~1a/bogusUse"),
		"and the census names it where it was written")
}

// TestPathItemRefSiblings_ReferenceKindsResolve pins that a reference written
// inside the sibling subtree is resolved before it is lowered: the loader never
// walks these nodes, so nothing else in the compile resolves them.
func TestPathItemRefSiblings_ReferenceKindsResolve(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, pathItemRefKindsSpec)
	openapitest.RequireNoErrorDiags(t, diags)

	getA := opByPath(t, doc, "GET", "/a")
	require.Len(t, getA.Params, 1, "a $ref'd parameter beside the $ref resolves")
	assert.Equal(t, "refParam", getA.Params[0].Name.Source)
	require.NotNil(t, getA.Request, "a $ref'd request body resolves")
	assert.Equal(t, "application/json", getA.Request.Contents[0].MediaType)
	require.Len(t, getA.Responses, 1, "a $ref'd response resolves")
	assert.Equal(t, 200, getA.Responses[0].Conditions.StatusCodes[0].From)
}

const pathItemRefKindsSpec = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a:
    $ref: '#/paths/~1b'
    get:
      operationId: getA
      parameters:
        - {$ref: '#/components/parameters/RefP'}
      requestBody: {$ref: '#/components/requestBodies/RefB'}
      responses:
        "200": {$ref: '#/components/responses/RefR'}
      callbacks:
        onEvent: {$ref: '#/components/callbacks/RefC'}
  /b:
    post:
      operationId: postB
      responses: {"200": {description: OK}}
components:
  parameters:
    RefP: {name: refParam, in: query, schema: {type: string}}
  requestBodies:
    RefB:
      content:
        application/json: {schema: {type: string}}
  responses:
    RefR:
      description: ok
      content:
        application/json: {schema: {type: string}}
  callbacks:
    RefC:
      '{$request.body#/url}':
        post:
          operationId: cbPost
          responses: {"200": {description: OK}}
`

// pathItemRefCollisionSpec declares the same constructs on both sides of one
// $ref: the method key, the summary, the servers, an x-* name, an undeclared key
// and an item parameter pair.
const pathItemRefCollisionSpec = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a:
    $ref: '#/paths/~1b'
    summary: use summary
    description: use description
    servers: [{url: https://use.example}]
    x-shared: use
    bogusBoth: {responses: {"200": {description: USE}}}
    parameters:
      - {name: dup, in: query, schema: {type: string}}
    get:
      operationId: useGet
      responses: {"200": {description: OK}}
  /b:
    summary: ref summary
    description: ref description
    servers: [{url: https://ref.example}]
    x-shared: ref
    bogusBoth: {responses: {"200": {description: REF}}}
    parameters:
      - {name: dup, in: query, schema: {type: string}}
    get:
      operationId: refGet
      responses: {"200": {description: OK}}
`

// TestPathItemRefSiblings_CollisionIsLoudAndUseSiteWins pins the collision rule
// GitHub #577 settles: OpenAPI leaves the result of a field written beside a
// $ref undefined, so the compiler applies the use-site precedence it already
// applies to a $ref-adjacent schema sibling, and reports the losing declaration
// rather than dropping it in silence.
func TestPathItemRefSiblings_CollisionIsLoudAndUseSiteWins(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, pathItemRefCollisionSpec)
	openapitest.RequireNoErrorDiags(t, diags)

	// One finding per colliding construct, each sited at the use site's own key
	// and naming the referent's.
	for _, at := range []string{
		"/paths/~1a/get",
		"/paths/~1a/summary",
		"/paths/~1a/description",
		"/paths/~1a/servers",
		"/paths/~1a/x-shared",
		"/paths/~1a/bogusBoth",
		"/paths/~1a/parameters/0",
	} {
		assert.True(t, openapitest.HasDiagCodeAt(diags, diag.PathItemRefCollision, at),
			"%s is reported at the use site's own key", at)
	}
	message := openapitest.DiagMessageAt(t, diags, diag.PathItemRefCollision,
		ir.SeverityWarning, "/paths/~1a/get")
	assert.Contains(t, message, "/paths/~1b/get", "the referent's position is named too")

	// The use site's declaration is the one lowered, and the referent's get is
	// not lowered at this mount at all.
	getA := opByPath(t, doc, "GET", "/a")
	assert.Equal(t, "useGet", getA.Name.Source)
	assert.Equal(t, jsontext.Pointer("/paths/~1a/get"), getA.Provenance.Pointer)
	getB := opByPath(t, doc, "GET", "/b")
	assert.Equal(t, "refGet", getB.Name.Source, "the referent still lowers at its own path")

	summary, ok := getA.Unmodeled["openapi:pathItemSummary"]
	require.True(t, ok)
	assert.JSONEq(t, `"use summary"`, string(summary.Value), "the use site's summary survives")
	servers, ok := getA.Unmodeled["openapi:servers"]
	require.True(t, ok)
	assert.JSONEq(t, `[{"url":"https://use.example"}]`, string(servers.Value))
	ext, ok := getA.Unmodeled["openapi:pathItem/x-shared"]
	require.True(t, ok)
	assert.JSONEq(t, `"use"`, string(ext.Value))

	// One operation at the mount, so no OpID is claimed twice.
	assert.False(t, openapitest.HasDiag(diags, diag.DuplicateOperationID))
	assert.False(t, openapitest.HasDiag(diags, diag.ConflictingOperationID))
}

// pathItemRefUnmountedSpec mounts no operation on either side of the reference:
// the use site writes item-level constructs and the referent declares none.
const pathItemRefUnmountedSpec = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a:
    $ref: '#/paths/~1b'
    servers: [{url: https://use.example}]
    x-beside: use
  /b:
    summary: ref summary
`

// TestPathItemRefSiblings_UnmountedKeepsSiblings pins the third carrier: an item
// that mounts no operation on either side has no operation to hold the use
// site's constructs, so they are kept on the nearest node that has an Unmodeled
// map and announced, exactly as a path item with no operations already was.
func TestPathItemRefSiblings_UnmountedKeepsSiblings(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, pathItemRefUnmountedSpec)
	openapitest.RequireNoErrorDiags(t, diags)

	svc := doc.Services[0]
	entry, ok := svc.Unmodeled["openapi:pathItem/paths/~1a/servers"]
	require.True(t, ok, "the use site's servers are kept on the service")
	assert.JSONEq(t, `[{"url":"https://use.example"}]`, string(entry.Value))
	ext, ok := svc.Unmodeled["openapi:pathItem/paths/~1a/x-beside"]
	require.True(t, ok)
	assert.JSONEq(t, `"use"`, string(ext.Value))
	assert.True(t, openapitest.HasDiagCodeAt(diags, diag.DegradedConstruct, "/paths/~1a"),
		"the preservation is announced at the use site's own pointer")
}

// pathItemRefBrokenSpec writes a reference beside the $ref that names nothing.
const pathItemRefBrokenSpec = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a:
    $ref: '#/paths/~1b'
    parameters:
      - {$ref: '#/components/parameters/Missing'}
    get:
      operationId: getA
      responses: {"200": {description: OK}}
      responses: {"200": {description: OK}}
  /b:
    post:
      operationId: postB
      responses: {"200": {description: OK}}
`

// TestPathItemRefSiblings_UnresolvedIsReported pins that a reference the loader
// never walks is resolved at the mount and reported when it does not resolve,
// rather than silently reaching the IR unresolved.
func TestPathItemRefSiblings_UnresolvedIsReported(t *testing.T) {
	t.Parallel()
	_, diags := parseFull(t, pathItemRefBrokenSpec)

	require.True(t, openapitest.HasDiagCodeAt(diags, diag.UnresolvedRef, "/paths/~1a"),
		"the failure is sited at the use site the reference was written beside")
}

// pathItemRefUnmountedWebhookSpec mounts nothing on either side of a webhook's
// $ref, so the use site's constructs have no operation to ride on.
const pathItemRefUnmountedWebhookSpec = `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
webhooks:
  hooked:
    $ref: '#/components/pathItems/Empty'
    x-beside: use
components:
  pathItems:
    Empty:
      summary: ref summary
`

// TestPathItemRefSiblings_UnmountedWebhookKeepsSiblings pins the orphan carrier
// on the webhook route: the use site's constructs land on the service, under the
// use site's own scope, and the preservation is announced there.
func TestPathItemRefSiblings_UnmountedWebhookKeepsSiblings(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, pathItemRefUnmountedWebhookSpec)
	openapitest.RequireNoErrorDiags(t, diags)

	entry, ok := doc.Services[0].Unmodeled["openapi:pathItem/webhooks/hooked/x-beside"]
	require.True(t, ok, "the webhook's sibling x-* is kept on the service")
	assert.JSONEq(t, `"use"`, string(entry.Value))
	assert.True(t, openapitest.HasDiagCodeAt(diags, diag.DegradedConstruct, "/webhooks/hooked"),
		"the preservation is announced at the use site's own pointer")
}

// pathItemRefUnmountedCallbackSpec mounts nothing on either side of a callback
// expression's $ref.
const pathItemRefUnmountedCallbackSpec = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /p:
    post:
      operationId: parent
      callbacks:
        onEvent:
          '{$request.body#/url}':
            $ref: '#/components/pathItems/Empty'
            x-beside: use
      responses: {"200": {description: OK}}
components:
  pathItems:
    Empty:
      summary: ref summary
`

// TestPathItemRefSiblings_UnmountedCallbackKeepsSiblings pins the orphan carrier
// on the callback route, which puts the kept constructs on the parent's HTTP
// binding, where the callback itself lives.
func TestPathItemRefSiblings_UnmountedCallbackKeepsSiblings(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, pathItemRefUnmountedCallbackSpec)
	openapitest.RequireNoErrorDiags(t, diags)

	const expr = "{$request.body#~1url}"
	parent := openapitest.FindOp(t, doc, "parent")
	entry, ok := parent.Bindings.HTTP[0].Unmodeled["openapi:pathItem/paths/~1p/post/callbacks/onEvent/"+expr+"/x-beside"]
	require.True(t, ok, "the expression's sibling x-* is kept on the parent's binding")
	assert.JSONEq(t, `"use"`, string(entry.Value))
	assert.True(t, openapitest.HasDiagCodeAt(diags, diag.DegradedConstruct,
		"/paths/~1p/post/callbacks/onEvent/"+expr),
		"the preservation is announced at the use site's own pointer")
}

// pathItemRefOpIDSpec has the operation written beside the $ref repeat the
// referent's operationId, which is two declarations of one id rather than one
// declaration mounted twice (GitHub #502).
const pathItemRefOpIDSpec = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a:
    $ref: '#/paths/~1b'
    get:
      operationId: sameId
      responses: {"200": {description: OK}}
  /b:
    get:
      operationId: sameId
      responses: {"200": {description: OK}}
`

// TestPathItemRefSiblings_RepeatedOpIDIsAConflict pins which of the two
// operationId findings a repeated id reaches: the sibling's operation is a
// declaration of its own, so repeating the referent's id is the document writing
// the id twice, not one declaration mounted twice.
func TestPathItemRefSiblings_RepeatedOpIDIsAConflict(t *testing.T) {
	t.Parallel()
	_, diags := parseFull(t, pathItemRefOpIDSpec)

	assert.True(t, openapitest.HasDiag(diags, diag.ConflictingOperationID),
		"two declarations writing one id is a conflict")
	assert.False(t, openapitest.HasDiag(diags, diag.DuplicateOperationID),
		"and not a remount: the sibling operation is not the referent's declaration")
}

// pathItemRefCollisionReversedSpec is pathItemRefCollisionSpec with the two
// paths declared the other way round, for the two-order diff.
const pathItemRefCollisionReversedSpec = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /b:
    summary: ref summary
    description: ref description
    servers: [{url: https://ref.example}]
    x-shared: ref
    bogusBoth: {responses: {"200": {description: REF}}}
    parameters:
      - {name: dup, in: query, schema: {type: string}}
    get:
      operationId: refGet
      responses: {"200": {description: OK}}
  /a:
    $ref: '#/paths/~1b'
    summary: use summary
    description: use description
    servers: [{url: https://use.example}]
    x-shared: use
    bogusBoth: {responses: {"200": {description: USE}}}
    parameters:
      - {name: dup, in: query, schema: {type: string}}
    get:
      operationId: useGet
      responses: {"200": {description: OK}}
`

// TestPathItemRefSiblings_CollisionIsOrderIndependent pins that what a colliding
// mount reports depends on the document and not on the order its two paths were
// declared in: the two fixtures differ only in that order, so the findings must
// be the same set of (code, pointer) pairs.
func TestPathItemRefSiblings_CollisionIsOrderIndependent(t *testing.T) {
	t.Parallel()
	_, forward := parseFull(t, pathItemRefCollisionSpec)
	_, reversed := parseFull(t, pathItemRefCollisionReversedSpec)

	assert.Equal(t, collisionFindings(t, forward), collisionFindings(t, reversed),
		"a collision is a property of the document, not of the order it was written in")
}

// collisionFindings returns the colliding constructs' findings as sorted
// "code pointer" strings, which is what must not depend on declaration order.
func collisionFindings(t *testing.T, diags []ir.Diagnostic) []string {
	t.Helper()
	var out []string
	for _, d := range diags {
		if d.Code == diag.PathItemRefCollision {
			out = append(out, d.Code+" "+string(d.Provenance.Pointer))
		}
	}
	slices.Sort(out)
	return out
}
