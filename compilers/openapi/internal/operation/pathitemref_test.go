package operation_test

import (
	"encoding/json/jsontext"
	"os"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
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
// route a path item is reached through, carrying the same item-level construct
// set the paths route's fixture does.
const pathItemRefWebhookSpec = `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
webhooks:
  hooked:
    $ref: '#/components/pathItems/Shared'
    summary: written beside the ref
    servers: [{url: https://use.example}]
    x-beside: use
    bogusUse: use
    parameters:
      - {name: useParam, in: query, schema: {type: string}}
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
// than paths. Every item-level construct the paths route asserts lands on every
// operation mounted here, under the hook's own use site.
func TestPathItemRefSiblings_WebhookRoute(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, pathItemRefWebhookSpec)
	openapitest.RequireNoErrorDiags(t, diags)

	sibling := openapitest.FindOp(t, doc, "siblingHook")
	assert.Equal(t, ir.OpID("op/openapi/webhooks/hooked/post"), sibling.ID)
	assert.Equal(t, jsontext.Pointer("/webhooks/hooked/post"), sibling.Provenance.Pointer)
	assert.True(t, sibling.Bindings.HTTP[0].IsWebhook, "a webhook operation keeps its binding")

	referent := openapitest.FindOp(t, doc, "refHook")
	assert.Equal(t, ir.OpID("op/openapi/webhooks/hooked/get"), referent.ID)

	for _, op := range []ir.Operation{sibling, referent} {
		assertPathItemSiblingConstructs(t, op, "/webhooks/hooked")
	}
	assert.True(t, openapitest.HasDiagCodeAt(diags, diag.UnknownObjectKey, "/webhooks/hooked/bogusUse"),
		"and the census names the use site's undeclared key where it was written")
}

// pathItemRefCallbackSpec is the same shape at a callback expression, the third
// route a path item is reached through, carrying the same item-level construct
// set the paths route's fixture does.
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
            summary: written beside the ref
            servers: [{url: https://use.example}]
            x-beside: use
            bogusUse: use
            parameters:
              - {name: useParam, in: query, schema: {type: string}}
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
// walked only from the mount that references it. As on the paths route, every
// item-level construct lands on every operation mounted at the expression, under
// the expression's own use site.
func TestPathItemRefSiblings_CallbackRoute(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, pathItemRefCallbackSpec)
	openapitest.RequireNoErrorDiags(t, diags)

	const usePtr = "/paths/~1p/post/callbacks/onEvent/{$request.body#~1url}"

	sibling := openapitest.FindOp(t, doc, "siblingCb")
	assert.Equal(t, jsontext.Pointer(usePtr+"/post"), sibling.Provenance.Pointer)

	referent := openapitest.FindOp(t, doc, "refCb")
	assert.Equal(t, jsontext.Pointer("/components/pathItems/Cb/get"), referent.Provenance.Pointer)

	for _, op := range []ir.Operation{sibling, referent} {
		assertPathItemSiblingConstructs(t, op, usePtr)
	}
	assert.True(t, openapitest.HasDiagCodeAt(diags, diag.UnknownObjectKey, usePtr+"/bogusUse"),
		"and the census names the use site's undeclared key where it was written")
}

// pathItemRefSiblingsExternalFixture is the external-ref half of the seam: its
// path item carries a $ref plus siblings, and the operation written beside the
// $ref writes a parameter reference that leaves the document for the target
// fixture beside it (resolve_target_path_item_sibling.yaml).
const pathItemRefSiblingsExternalFixture = "../../../../testdata/openapi/resolve_main_path_item_sibling.yaml"

// TestPathItemRefSiblings_ExternalRefFollowsTheCompilesPolicy pins the loader's
// own external-reference policy onto the seam. A sibling subtree is read from
// raw nodes the loader never modelled, so only the seam can resolve a reference
// written inside it — which means it must follow the same allow-external-refs
// option the loader follows, and must report what it cannot resolve rather than
// drop it. With the option the target file's component lands on the IR at the
// sibling operation; without it the reference is an unresolved-ref diagnostic
// sited at the use site.
func TestPathItemRefSiblings_ExternalRefFollowsTheCompilesPolicy(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile(pathItemRefSiblingsExternalFixture)
	require.NoError(t, err)

	compiled := func(allow bool) (*ir.Document, []ir.Diagnostic) {
		t.Helper()
		doc, diags, err := openapi.New().Compile(t.Context(),
			[]compilers.Source{{Path: pathItemRefSiblingsExternalFixture, Data: append([]byte(nil), data...)}},
			compilers.Options{FormatOptions: openapi.Options{AllowExternalRefs: allow}})
		require.NoError(t, err)
		require.NotNil(t, doc)
		return doc, diags
	}

	allowed, allowedDiags := compiled(true)
	openapitest.RequireNoErrorDiags(t, allowedDiags)
	getA := opByPath(t, allowed, "GET", "/a")
	require.Len(t, getA.Params, 1, "the reference beside the $ref follows into the second file")
	assert.Equal(t, "extSibling", getA.Params[0].Name.Source)

	refused, refusedDiags := compiled(false)
	assert.True(t, openapitest.HasDiagCodeAt(refusedDiags, diag.UnresolvedRef, "/paths/~1a"),
		"without the option the sibling's external reference is reported, not dropped")
	refusedA := opByPath(t, refused, "GET", "/a")
	assert.Empty(t, refusedA.Params, "and no unresolved parameter reaches the IR")
}

// pathItemRefItemSpec declares item-level constructs beside a $ref: servers, an
// x-* extension, an undeclared key and an item-level parameter, none of which
// the reference wrapper models.
const pathItemRefItemSpec = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a:
    $ref: '#/paths/~1b'
    summary: written beside the ref
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
		assertPathItemSiblingConstructs(t, op, "/paths/~1a")
	}
	assert.True(t, openapitest.HasDiagCodeAt(diags, diag.UnknownObjectKey, "/paths/~1a/bogusUse"),
		"and the census names it where it was written")
}

// assertPathItemSiblingConstructs requires the full item-level construct set a
// use site writes beside a $ref to have reached op, each under the use site's
// own pointer: the summary, the servers, an x-* extension, an undeclared key,
// and the item parameter drawn from the use site's YAML.
//
// It is shared by the paths, webhook and callback route tests so the three
// cannot assert different constructs: what one route forgets is what all three
// fail to notice, which is exactly how the servers half came to be missing on
// two of the three routes (GitHub #39).
func assertPathItemSiblingConstructs(t *testing.T, op ir.Operation, usePtr string) {
	t.Helper()
	where := op.Name.Source

	summary, ok := op.Unmodeled["openapi:pathItemSummary"]
	require.True(t, ok, "%s keeps the use site's path-item summary", where)
	assert.JSONEq(t, `"written beside the ref"`, string(summary.Value))
	assert.Equal(t, jsontext.Pointer(usePtr+"/summary"), summary.Provenance.Pointer)

	servers, ok := op.Unmodeled["openapi:servers"]
	require.True(t, ok, "%s keeps the use site's servers", where)
	assert.JSONEq(t, `[{"url":"https://use.example"}]`, string(servers.Value))
	assert.Equal(t, jsontext.Pointer(usePtr+"/servers"), servers.Provenance.Pointer)

	ext, ok := op.Unmodeled["openapi:pathItem/x-beside"]
	require.True(t, ok, "%s keeps the use site's x-*", where)
	assert.JSONEq(t, `"use"`, string(ext.Value))
	assert.Equal(t, jsontext.Pointer(usePtr+"/x-beside"), ext.Provenance.Pointer)

	unknown, ok := op.Unmodeled["openapi:pathItem/bogusUse"]
	require.True(t, ok, "%s keeps the use site's undeclared key", where)
	assert.JSONEq(t, `"use"`, string(unknown.Value))
	assert.Equal(t, jsontext.Pointer(usePtr+"/bogusUse"), unknown.Provenance.Pointer)

	require.Len(t, op.Params, 1, "%s merges the use site's item parameter", where)
	assert.Equal(t, "useParam", op.Params[0].Name.Source)
	assert.Equal(t, jsontext.Pointer(usePtr+"/parameters/0"), op.Params[0].Provenance.Pointer)
}

// TestPathItemRefSiblings_ReferenceKindsResolve pins that a reference written
// inside the sibling subtree is resolved before it is lowered: the loader never
// walks these nodes, so nothing else in the compile resolves them.
//
// Every reference kind a path-item subtree can reach is exercised here — a
// parameter, a request body, a response and a callback on the sibling operation,
// and a header, a link and an example on a response written beside the $ref. The
// header and the example land on the IR. The link does not, because a Link
// Object lowers to no node of its own anywhere in this compiler: what reaches
// the IR for it is the verbatim map preserveResponseExtras keeps, and the
// resolving half is pinned by TestPathItemRefSiblings_UnresolvedLinkIsReported
// beside it (GitHub #275).
func TestPathItemRefSiblings_ReferenceKindsResolve(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, pathItemRefKindsSpec)
	openapitest.RequireNoErrorDiags(t, diags)

	getA := opByPath(t, doc, "GET", "/a")
	require.Len(t, getA.Params, 1, "a $ref'd parameter beside the $ref resolves")
	assert.Equal(t, "refParam", getA.Params[0].Name.Source)
	require.NotNil(t, getA.Request, "a $ref'd request body resolves")
	assert.Equal(t, "application/json", getA.Request.Contents[0].MediaType)

	success := responseByStatus(t, getA, 200)
	assert.Equal(t, 200, success.Conditions.StatusCodes[0].From, "a $ref'd response resolves")

	created := responseByStatus(t, getA, 201)
	require.Len(t, created.Headers, 1, "a $ref'd header on a sibling response resolves")
	assert.Equal(t, "X-Rate", created.Headers[0].WireName)

	require.NotNil(t, created.Payload, "the response written beside the $ref keeps its body")
	require.NotEmpty(t, created.Payload.Contents)
	examples := created.Payload.Contents[0].Examples
	require.Len(t, examples, 1, "a $ref'd example on a sibling media type resolves")
	assert.Equal(t, "refEx", examples[0].Name)
	require.NotNil(t, examples[0].Value, "the referenced example's own value is lowered")

	links, ok := created.Unmodeled["openapi:links"]
	require.True(t, ok, "the sibling response's $ref'd link is kept verbatim")
	assert.Contains(t, string(links.Value), "RefL",
		"a Link Object has no IR home, so the map reaches the IR under its own key")
}

// responseByStatus returns the single success response an operation declares for
// code, requiring it to be there rather than indexing into a list that a second
// response would have made ambiguous.
func responseByStatus(t *testing.T, op ir.Operation, code int) ir.Response {
	t.Helper()
	for _, r := range op.Responses {
		for _, sc := range r.Conditions.StatusCodes {
			if sc.From == code && sc.To == code {
				return r
			}
		}
	}
	t.Fatalf("operation %s declares no response for status %d", op.ID, code)
	return ir.Response{}
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
        "201":
          description: created
          headers:
            X-Rate: {$ref: '#/components/headers/RefH'}
          content:
            application/json:
              schema: {type: string}
              examples:
                refEx: {$ref: '#/components/examples/RefE'}
          links:
            next: {$ref: '#/components/links/RefL'}
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
  headers:
    RefH: {schema: {type: integer}}
  examples:
    RefE: {value: {from: component}}
  links:
    RefL: {operationId: getA}
  callbacks:
    RefC:
      '{$request.body#/url}':
        post:
          operationId: cbPost
          responses: {"200": {description: OK}}
`

// pathItemRefDanglingLinkSpec writes a $ref'd link on a sibling response whose
// target names nothing.
const pathItemRefDanglingLinkSpec = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a:
    $ref: '#/paths/~1b'
    get:
      operationId: getA
      responses:
        "200":
          description: ok
          links:
            next: {$ref: '#/components/links/Missing'}
  /b:
    post:
      operationId: postB
      responses: {"200": {description: OK}}
`

// TestPathItemRefSiblings_UnresolvedLinkIsReported pins that the seam reaches a
// link reference written in the sibling subtree: a link whose declaration is
// absent is reported like any other unresolved reference there, which is what
// makes the resolving half of the kinds fixture above mean something. It is also
// the one reference kind whose resolution no IR field records — nothing reads a
// resolved link — so the diagnostic is the only evidence that it happened.
func TestPathItemRefSiblings_UnresolvedLinkIsReported(t *testing.T) {
	t.Parallel()
	_, diags := parseFull(t, pathItemRefDanglingLinkSpec)
	assert.True(t, openapitest.HasDiagCodeAt(diags, diag.UnresolvedRef, "/paths/~1a"),
		"the link reference the loader never walks is resolved here and reported")
}

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
