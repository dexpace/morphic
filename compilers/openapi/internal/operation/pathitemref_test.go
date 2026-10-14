package operation_test

import (
	"encoding/json/jsontext"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irverify"
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
    bogusUse: {responses: {"200": {description: use}}}
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

// TestPathItemRefSiblings_WebhookRoute holds the webhook route to the paths
// route's reading: the same model and the same carriers, reached under webhooks
// rather than paths. Every item-level construct the paths route asserts lands on every
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
            bogusUse: {responses: {"200": {description: use}}}
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

// TestPathItemRefSiblings_CallbackRoute holds the callback route to the paths
// route's reading: every item-level construct lands on every operation mounted
// at the expression, under the expression's own use site.
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

// pathItemRefSiblingsExternalFixture's path item carries a $ref plus siblings,
// and the operation written beside the $ref writes a parameter reference that
// leaves the document for the target fixture beside it
// (resolve_target_path_item_sibling.yaml).
const pathItemRefSiblingsExternalFixture = "../../../../testdata/openapi/resolve_main_path_item_sibling.yaml"

// TestPathItemRefSiblings_ExternalRefFollowsTheCompilesPolicy pins that a
// reference written beside a $ref follows the compile's external-reference
// policy, as every reference the load phase resolves does. With the option the
// target file's component lands on the IR at the sibling operation; without it
// the reference is reported unresolved once, where it is written.
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
	assert.Equal(t, []string{"/paths/~1a/get/parameters/0"}, unresolvedAt(refusedDiags),
		"without the option the sibling's external reference is reported once, where it is written")
	refusedA := opByPath(t, refused, "GET", "/a")
	assert.Empty(t, refusedA.Params, "and no unresolved parameter reaches the IR")
}

// crossDocumentTarget is a path item in another document whose item-level
// parameter's schema names that document's own S, which the referring
// document also declares, differently.
const crossDocumentTarget = `openapi: 3.1.0
info: {title: Target, version: "1"}
paths:
  /b:
    parameters:
      - {name: inl, in: header, schema: {$ref: '#/components/schemas/S'}}
    post:
      operationId: postB
      responses: {"200": {description: OK}}
components:
  schemas:
    S: {type: integer}
`

// crossDocumentUse refers to crossDocumentTarget's path item and writes an
// item parameter and an operation with one of its own beside the $ref.
const crossDocumentUse = `openapi: 3.1.0
info: {title: Main, version: "1"}
paths:
  /a:
    $ref: './target.yaml#/paths/~1b'
    parameters:
      - {$ref: '#/components/parameters/P'}
    get:
      operationId: getA
      parameters:
        - {$ref: '#/components/parameters/Q'}
      responses: {"200": {description: OK}}
components:
  parameters:
    P: {name: mainP, in: query, schema: {type: string}}
    Q: {name: mainQ, in: query, schema: {type: string}}
  schemas:
    S: {type: string}
`

// TestPathItemRefSiblings_EachSideReadsInItsOwnDocument pins the scope each
// side of a cross-document $ref lowers in, since every operation at the mount
// carries both sides' item parameters. Read in the source, the referent's
// parameter on the use site's operation bound the source's S; read in the
// referent's document, the use site's parameters lost the pointer of their
// declaration.
func TestPathItemRefSiblings_EachSideReadsInItsOwnDocument(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "target.yaml"), []byte(crossDocumentTarget), 0o600))
	use := filepath.Join(dir, "main.yaml")
	doc, _, err := openapi.New().Compile(t.Context(),
		[]compilers.Source{{Path: use, Data: []byte(crossDocumentUse)}},
		compilers.Options{FormatOptions: openapi.Options{AllowExternalRefs: true}})
	require.NoError(t, err)
	require.NotNil(t, doc)

	postB, getA := opByPath(t, doc, "POST", "/a"), opByPath(t, doc, "GET", "/a")
	referent := paramNamed(t, getA, "inl")
	assert.Equal(t, paramNamed(t, postB, "inl").Type, referent.Type,
		"the referent's parameter binds the same type on either side's operation")
	assert.NotEqual(t, ir.TypeID("t/openapi/components/schemas/S"), referent.Type.Target,
		"and never the source's S")
	assert.Equal(t, jsontext.Pointer("/components/parameters/P"), paramNamed(t, postB, "mainP").Provenance.Pointer,
		"the use site's parameter keeps its declaration's pointer on the referent's operation")
	assert.Equal(t, jsontext.Pointer("/components/parameters/Q"), paramNamed(t, getA, "mainQ").Provenance.Pointer,
		"and the use site's operation reads its own references in the source")
}

// paramNamed returns op's parameter whose source name is name.
func paramNamed(t *testing.T, op ir.Operation, name string) ir.Parameter {
	t.Helper()
	for _, p := range op.Params {
		if p.Name.Source == name {
			return p
		}
	}
	require.Failf(t, "parameter not found", "%s has no parameter %q", op.Name.Source, name)
	return ir.Parameter{}
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
    bogusUse: {responses: {"200": {description: use}}}
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
	assert.JSONEq(t, `{"responses":{"200":{"description":"use"}}}`, string(unknown.Value))
	assert.Equal(t, jsontext.Pointer(usePtr+"/bogusUse"), unknown.Provenance.Pointer)

	require.Len(t, op.Params, 1, "%s merges the use site's item parameter", where)
	assert.Equal(t, "useParam", op.Params[0].Name.Source)
	assert.Equal(t, jsontext.Pointer(usePtr+"/parameters/0"), op.Params[0].Provenance.Pointer)
}

// TestPathItemRefSiblings_ReferenceKindsResolve pins that a reference written
// inside the fields beside a $ref is resolved before it is lowered, which the
// load phase does with the rest of the document.
//
// It exercises every reference kind a path-item subtree reaches: a parameter, a
// request body, a response and a callback on the sibling operation, and a
// header, a link and an example on its response. A Link Object lowers to no
// node of its own, only to preserveResponseExtras' verbatim map, so its
// resolving half is TestPathItemRefSiblings_UnresolvedLinkIsReported
// (GitHub #275).
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

	cbPost := openapitest.FindOp(t, doc, "cbPost")
	assert.Equal(t, ir.OpID("op/openapi/paths/~1a/get/callbacks/onEvent/{$request.body#~1url}/post"), cbPost.ID,
		"a $ref'd callback resolves, and its operation mounts under the operation beside the $ref")
	require.Len(t, getA.Bindings.HTTP[0].Callbacks, 1)
	assert.Equal(t, []ir.OpID{cbPost.ID}, getA.Bindings.HTTP[0].Callbacks[0].Operations)
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

// TestPathItemRefSiblings_UnresolvedLinkIsReported pins that resolution reaches
// a link reference written beside a $ref: a link whose declaration is
// absent is reported like any other unresolved reference there, which is what
// makes the resolving half of the kinds fixture above mean something. It is also
// the one reference kind whose resolution no IR field records — nothing reads a
// resolved link — so the diagnostic is the only evidence that it happened.
func TestPathItemRefSiblings_UnresolvedLinkIsReported(t *testing.T) {
	t.Parallel()
	_, diags := parseFull(t, pathItemRefDanglingLinkSpec)
	assert.Equal(t, []string{"/paths/~1a/get/responses/200/links/next"}, unresolvedAt(diags),
		"the link reference beside the $ref is resolved with the document and reported where it is written")
}

// pathItemRefCollisionSpec declares the same constructs on both sides of one
// $ref: the method key, the summary, the servers, an x-* name, an undeclared key
// and an item parameter pair, typed apart so the one lowered names its side.
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
      - {name: dup, in: query, schema: {type: integer}}
    get:
      operationId: refGet
      responses: {"200": {description: OK}}
`

// TestPathItemRefSiblings_CollisionIsLoudAndUseSiteWins pins the collision rule
// GitHub #577 settles: OpenAPI leaves the result of a field written beside a
// $ref undefined, so the use site's declaration wins every construct, as a
// $ref-adjacent schema sibling's does, and the losing one is reported rather
// than dropped in silence.
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
	bogus, ok := getA.Unmodeled["openapi:pathItem/bogusBoth"]
	require.True(t, ok)
	assert.JSONEq(t, `{"responses":{"200":{"description":"USE"}}}`, string(bogus.Value),
		"an undeclared key is the use site's too, as every other construct is")
	assert.Equal(t, jsontext.Pointer("/paths/~1a/bogusBoth"), bogus.Provenance.Pointer)
	assert.False(t, openapitest.HasDiag(diags, diag.UnknownKeyEntryTaken),
		"the referent's key is not written, so nothing is left to say it was dropped")
	assert.Contains(t, openapitest.DiagMessageAt(t, diags, diag.PathItemRefCollision, ir.SeverityWarning,
		"/paths/~1a/parameters/0"), `the parameter "dup" in query here`, "a parameter is named as written")
	require.Len(t, getA.Params, 1, "the referent's parameter is not lowered beside the use site's")
	assert.Equal(t, ir.TypeID("t/prim/string"), getA.Params[0].Type.Target, "the use site's is the one kept")
	assert.Equal(t, jsontext.Pointer("/paths/~1a/parameters/0"), getA.Params[0].Provenance.Pointer)

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
  /b:
    post:
      operationId: postB
      responses: {"200": {description: OK}}
`

// TestPathItemRefSiblings_UnresolvedIsReported pins that a reference written
// beside a $ref that does not resolve is reported once, where it is written,
// rather than silently reaching the IR unresolved.
func TestPathItemRefSiblings_UnresolvedIsReported(t *testing.T) {
	t.Parallel()
	_, diags := parseFull(t, pathItemRefBrokenSpec)

	assert.Equal(t, []string{"/paths/~1a/parameters/0"}, unresolvedAt(diags),
		"the failure is reported once, at the reference written beside the $ref")
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
      - {name: dup, in: query, schema: {type: integer}}
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
	forwardDoc, forward := parseFull(t, pathItemRefCollisionSpec)
	reversedDoc, reversed := parseFull(t, pathItemRefCollisionReversedSpec)

	assert.Equal(t, collisionFindings(t, forward), collisionFindings(t, reversed),
		"a collision is a property of the document, not of the order it was written in")
	for _, at := range []struct{ method, uri string }{{"GET", "/a"}, {"GET", "/b"}} {
		want, got := opByPath(t, forwardDoc, at.method, at.uri), opByPath(t, reversedDoc, at.method, at.uri)
		assert.Empty(t, cmp.Diff(want, got), "what %s %s lowers to, the winners included, follows no order", at.method, at.uri)
	}
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

// unresolvedAt returns the pointer of every unresolved-ref finding, in the
// order they were reported.
func unresolvedAt(diags []ir.Diagnostic) []string {
	var at []string
	for _, d := range diags {
		if d.Code == diag.UnresolvedRef {
			at = append(at, string(d.Provenance.Pointer))
		}
	}
	return at
}

// pathItemRefSharedCallbackSpec has two operations share one $ref'd callback
// whose expression writes an operation beside its $ref, and a components path
// item carrying such a callback inline mounted at a path and a webhook.
const pathItemRefSharedCallbackSpec = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /one:
    post:
      callbacks: {cb: {$ref: '#/components/callbacks/CB'}}
      responses: {"200": {description: OK}}
  /two:
    post:
      callbacks: {cb: {$ref: '#/components/callbacks/CB'}}
      responses: {"200": {description: OK}}
  /three: {$ref: '#/components/pathItems/Inline'}
webhooks:
  hook: {$ref: '#/components/pathItems/Inline'}
components:
  pathItems:
    Target:
      post: {responses: {"200": {description: OK}}}
    Inline:
      post:
        callbacks:
          cb:
            '{$request.body#/url}':
              $ref: '#/components/pathItems/Target'
              put: {responses: {"200": {description: OK}}}
        responses: {"200": {description: OK}}
  callbacks:
    CB:
      '{$request.body#/url}':
        $ref: '#/components/pathItems/Target'
        put: {responses: {"200": {description: OK}}}
`

// TestPathItemRefSiblings_ASharedCallbackMountsPerParent pins the identity of an
// operation written beside a callback expression's $ref: it mounts under each
// parent, as the referenced item's own do, and is declared once, where the
// expression writes it. Mounted at the declaration, every parent sharing the
// callback produced one operation ID.
func TestPathItemRefSiblings_ASharedCallbackMountsPerParent(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, pathItemRefSharedCallbackSpec)
	openapitest.RequireNoErrorDiags(t, diags)
	assert.Empty(t, irverify.Verify(doc), "no operation ID is declared twice")

	const expr = "/callbacks/cb/{$request.body#~1url}/put"
	puts := map[ir.OpID]jsontext.Pointer{}
	for _, g := range doc.Services[0].Groups {
		for _, op := range g.Operations {
			if strings.HasSuffix(string(op.ID), expr) {
				puts[op.ID] = op.Provenance.Pointer
			}
		}
	}
	assert.Equal(t, map[ir.OpID]jsontext.Pointer{
		"op/openapi/paths/~1one/post" + expr:   "/components/callbacks/CB/{$request.body#~1url}/put",
		"op/openapi/paths/~1two/post" + expr:   "/components/callbacks/CB/{$request.body#~1url}/put",
		"op/openapi/paths/~1three/post" + expr: "/components/pathItems/Inline/post/callbacks/cb/{$request.body#~1url}/put",
		"op/openapi/webhooks/hook/post" + expr: "/components/pathItems/Inline/post/callbacks/cb/{$request.body#~1url}/put",
	}, puts, "one operation per parent, each declared where the expression writes it")
}

// pathItemRefUnmountedCollisionSpec declares the same constructs on both sides
// of a $ref neither side mounts an operation for.
const pathItemRefUnmountedCollisionSpec = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a:
    $ref: '#/paths/~1b'
    summary: use summary
    x-both: use
    bogusBoth: {responses: {"200": {description: USE}}}
  /b:
    summary: ref summary
    x-both: ref
    bogusBoth: {responses: {"200": {description: REF}}}
`

// TestPathItemRefSiblings_UnmountedCollisionKeepsTheUseSite pins precedence on
// the carrier an item mounting no operation uses: the use site keeps every
// construct there too, and a summary, which that carrier keeps from neither
// side, is reported as colliding nowhere.
func TestPathItemRefSiblings_UnmountedCollisionKeepsTheUseSite(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, pathItemRefUnmountedCollisionSpec)
	openapitest.RequireNoErrorDiags(t, diags)

	svc := doc.Services[0]
	for key, want := range map[string]string{
		"openapi:pathItem/paths/~1a/x-both":    `"use"`,
		"openapi:pathItem/paths/~1a/bogusBoth": `{"responses":{"200":{"description":"USE"}}}`,
	} {
		entry, ok := svc.Unmodeled[key]
		require.True(t, ok, key)
		assert.JSONEq(t, want, string(entry.Value), key)
		assert.True(t, strings.HasPrefix(string(entry.Provenance.Pointer), "/paths/~1a/"), key)
	}
	assert.True(t, openapitest.HasDiagCodeAt(diags, diag.PathItemRefCollision, "/paths/~1a/x-both"))
	assert.True(t, openapitest.HasDiagCodeAt(diags, diag.PathItemRefCollision, "/paths/~1a/bogusBoth"))
	assert.False(t, openapitest.HasDiagCodeAt(diags, diag.PathItemRefCollision, "/paths/~1a/summary"),
		"neither summary is kept, so neither is the one kept")
	assert.False(t, openapitest.HasDiag(diags, diag.UnknownKeyEntryTaken))
}

// pathItemRefChainSpec chains /a to /b to /c, with fields beside both $refs.
const pathItemRefChainSpec = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a:
    $ref: '#/paths/~1b'
    summary: from a
    get:
      operationId: getFromA
      responses: {"200": {description: OK}}
  /b:
    $ref: '#/paths/~1c'
    summary: from b
    servers: [{url: https://b.example}]
    get:
      operationId: getFromB
      responses: {"200": {description: OK}}
    put:
      operationId: putFromB
      responses: {"200": {description: OK}}
  /c:
    get:
      operationId: getFromC
      responses: {"200": {description: OK}}
    post:
      operationId: postFromC
      responses: {"200": {description: OK}}
`

// TestPathItemRefSiblings_AChainCarriesTheFieldsBesideEachRef pins a chain: the
// path item a $ref names is that item with the fields beside its own $ref, so
// /a mounts /b's put and /c's post, takes /b's servers, and keeps its own get
// and summary over both deeper ones, each loser named once.
func TestPathItemRefSiblings_AChainCarriesTheFieldsBesideEachRef(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, pathItemRefChainSpec)
	openapitest.RequireNoErrorDiags(t, diags)
	assert.Empty(t, irverify.Verify(doc))

	for method, decl := range map[string]jsontext.Pointer{
		"GET": "/paths/~1a/get", "PUT": "/paths/~1b/put", "POST": "/paths/~1c/post",
	} {
		op := opByPath(t, doc, method, "/a")
		assert.Equal(t, decl, op.Provenance.Pointer, "%s at /a is declared where the chain writes it", method)
		summary, ok := op.Unmodeled["openapi:pathItemSummary"]
		require.True(t, ok)
		assert.JSONEq(t, `"from a"`, string(summary.Value))
		servers, ok := op.Unmodeled["openapi:servers"]
		require.True(t, ok, "%s at /a carries the servers beside /b's $ref", method)
		assert.JSONEq(t, `[{"url":"https://b.example"}]`, string(servers.Value))
	}
	var collided []string
	for _, d := range diags {
		if d.Code == diag.PathItemRefCollision {
			collided = append(collided, string(d.Provenance.Pointer)+" "+d.Message[strings.Index(d.Message, "declares at ")+len("declares at "):strings.Index(d.Message, ";")])
		}
	}
	slices.Sort(collided)
	assert.Equal(t, []string{
		"/paths/~1a/get /paths/~1b/get",
		"/paths/~1a/get /paths/~1c/get",
		"/paths/~1a/summary /paths/~1b/summary",
		"/paths/~1b/get /paths/~1c/get",
	}, collided, "each declaration a nearer one takes over is named once, at the one kept")
}

// TestPathItemRefSiblings_FieldsAnotherDocumentWritesAreReported pins the chain
// this compiler cannot read all of: the fields beside a $ref in another
// document are not modelled, and saying so is what keeps them from being
// dropped in silence.
func TestPathItemRefSiblings_FieldsAnotherDocumentWritesAreReported(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "other.yaml"), []byte(`openapi: 3.1.0
info: {title: O, version: "1"}
paths:
  /x:
    $ref: '#/components/pathItems/P'
    put: {responses: {"200": {description: OK}}}
components:
  pathItems:
    P:
      get: {responses: {"200": {description: OK}}}
`), 0o600))
	doc, diags, err := openapi.New().Compile(t.Context(),
		[]compilers.Source{{Path: filepath.Join(dir, "main.yaml"), Data: []byte(`openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a: {$ref: './other.yaml#/paths/~1x'}
`)}}, compilers.Options{FormatOptions: openapi.Options{AllowExternalRefs: true}})
	require.NoError(t, err)
	require.NotNil(t, doc)

	opByPath(t, doc, "GET", "/a")
	message := openapitest.DiagMessageAt(t, diags, diag.DegradedConstruct, ir.SeverityWarning, "/paths/~1a")
	assert.Contains(t, message, "./other.yaml#/paths/~1x", "the reference reaching the unread fields is named")
	assert.Contains(t, message, "in another document")
	assert.Contains(t, message, "not lowered")
}

// TestPathItemRefSiblings_FieldsTheWalkDoesNotReachAreReported pins the other
// chain this compiler cannot read all of: a hop in this document at a position
// no object its walk holds, here under an x-* key, is not modelled either, and
// the report says where it is rather than calling it another document's.
func TestPathItemRefSiblings_FieldsTheWalkDoesNotReachAreReported(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, `openapi: 3.1.0
info: {title: T, version: "1"}
x-shared:
  item:
    $ref: '#/paths/~1b'
    put: {operationId: putShared, responses: {"200": {description: OK}}}
paths:
  /a: {$ref: '#/x-shared/item'}
  /b:
    get: {operationId: getB, responses: {"200": {description: OK}}}
`)
	openapitest.RequireNoErrorDiags(t, diags)

	assert.Equal(t, "getB", opByPath(t, doc, "GET", "/a").Name.Source, "the item the chain ends at still mounts")
	message := openapitest.DiagMessageAt(t, diags, diag.DegradedConstruct, ir.SeverityWarning, "/paths/~1a")
	assert.Contains(t, message, "#/x-shared/item", "the reference reaching the unread fields is named")
	assert.Contains(t, message, "does not reach")
	assert.NotContains(t, message, "another document", "the hop is this document's")
}

// pathItemRefCallbackSiblingSpec writes an expression beside a Callback
// Object's $ref, which OpenAPI says to ignore: a Reference Object's other
// fields are not the callback's.
const pathItemRefCallbackSiblingSpec = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /one:
    post:
      operationId: one
      callbacks:
        cb:
          $ref: '%s#/components/callbacks/CB'
          '{$request.body#/url}':
            $ref: '#/components/pathItems/T'
            patch:
              operationId: ignoredSibling
              responses: {"200": {description: OK}}
      responses: {"200": {description: OK}}
components:
  pathItems:
    T:
      post: {responses: {"200": {description: OK}}}
  callbacks:
    CB:
      '{$request.body#/url}':
        post:
          operationId: cbPost
          responses: {"200": {description: OK}}
`

// TestPathItemRefSiblings_AnExpressionBesideACallbackRefIsIgnored pins where
// the fields beside a $ref are read from: the source, at the $ref that writes
// them, never at a position another document's content was mounted at, so an
// expression beside a Callback Object's $ref is ignored however the callback
// resolves.
func TestPathItemRefSiblings_AnExpressionBesideACallbackRefIsIgnored(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	other := strings.Replace(pathItemRefCallbackSiblingSpec, "%s", "", 1)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "other.yaml"), []byte(other), 0o600))
	for _, at := range []string{"", "./other.yaml"} {
		doc, _, err := openapi.New().Compile(t.Context(),
			[]compilers.Source{{Path: filepath.Join(dir, "main.yaml"), Data: []byte(strings.Replace(pathItemRefCallbackSiblingSpec, "%s", at, 1))}},
			compilers.Options{FormatOptions: openapi.Options{AllowExternalRefs: true}})
		require.NoError(t, err)
		require.NotNil(t, doc)
		openapitest.FindOp(t, doc, "cbPost")
		for _, g := range doc.Services[0].Groups {
			for _, op := range g.Operations {
				assert.NotEqual(t, "ignoredSibling", op.Name.Source, "callback %q", at)
			}
		}
	}
}

// TestPathItemRefSiblings_AForeignReferentKeepsItsOwnPositions pins the one
// mount where both declarations would share pointers: a use site writing item
// parameters beside a $ref to another document's path item, whose content is
// placed at the use site because no pointer here addresses it. Each parameter
// keeps the type its own schema declares.
func TestPathItemRefSiblings_AForeignReferentKeepsItsOwnPositions(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "target.yaml"), []byte(`openapi: 3.1.0
info: {title: Target, version: "1"}
paths:
  /b:
    parameters:
      - {name: refP, in: header, schema: {type: object, properties: {fromRef: {type: integer}}}}
    get:
      operationId: refGet
      responses: {"200": {description: OK}}
    post:
      operationId: postB
      responses: {"200": {description: OK}}
`), 0o600))
	doc, diags, err := openapi.New().Compile(t.Context(),
		[]compilers.Source{{Path: filepath.Join(dir, "main.yaml"), Data: []byte(`openapi: 3.1.0
info: {title: Main, version: "1"}
paths:
  /a:
    $ref: './target.yaml#/paths/~1b'
    parameters:
      - {name: useP, in: query, schema: {type: object, properties: {fromUse: {type: string}}}}
    get:
      operationId: useGet
      responses: {"200": {description: OK}}
`)}}, compilers.Options{FormatOptions: openapi.Options{AllowExternalRefs: true}})
	require.NoError(t, err)
	require.NotNil(t, doc)
	assert.Empty(t, irverify.Verify(doc))

	props := func(p ir.Parameter) []string {
		t.Helper()
		def, ok := doc.Types[p.Type.Target]
		require.True(t, ok, "%s has a type", p.Name.Source)
		model, ok := def.(*ir.Model)
		require.True(t, ok)
		names := make([]string, 0, len(model.Properties))
		for _, prop := range model.Properties {
			names = append(names, prop.Name.Source)
		}
		return names
	}
	for _, op := range []ir.Operation{opByPath(t, doc, "GET", "/a"), opByPath(t, doc, "POST", "/a")} {
		assert.Equal(t, []string{"fromUse"}, props(paramNamed(t, op, "useP")))
		assert.Equal(t, []string{"fromRef"}, props(paramNamed(t, op, "refP")),
			"the referent's parameter keeps its own schema, not the use site's at the same index")
	}
	message := openapitest.DiagMessageAt(t, diags, diag.PathItemRefCollision, ir.SeverityWarning, "/paths/~1a/get")
	assert.Contains(t, message, "./target.yaml#/paths/~1b/get", "another document's position is named by the reference")
}

// TestPathItemRefSiblings_AWholeDocumentReferentIsNamedByItsFragment pins how a
// finding names a position in a document a $ref takes whole, the common shape
// of a path split into its own file: with the empty fragment, so the pointer
// after it reads as one rather than as a file path.
func TestPathItemRefSiblings_AWholeDocumentReferentIsNamedByItsFragment(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "item.yaml"), []byte(`get:
  operationId: itemGet
  responses: {"200": {description: OK}}
`), 0o600))
	doc, diags, err := openapi.New().Compile(t.Context(),
		[]compilers.Source{{Path: filepath.Join(dir, "main.yaml"), Data: []byte(`openapi: 3.1.0
info: {title: Main, version: "1"}
paths:
  /a:
    $ref: './item.yaml'
    get:
      operationId: useGet
      responses: {"200": {description: OK}}
`)}}, compilers.Options{FormatOptions: openapi.Options{AllowExternalRefs: true}})
	require.NoError(t, err)
	require.NotNil(t, doc)

	message := openapitest.DiagMessageAt(t, diags, diag.PathItemRefCollision, ir.SeverityWarning, "/paths/~1a/get")
	assert.Contains(t, message, "at ./item.yaml#/get;", "the position is the fragment of the document named")
}

// TestPathItemRefSiblings_AnAliasDeclaresAtItsOwnPosition pins where an alias
// of a use site declares the fields beside its $ref: at the alias's own path,
// as an aliased inline item declares there, not where the load phase first met
// the mapping.
func TestPathItemRefSiblings_AnAliasDeclaresAtItsOwnPosition(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, item string }{
		{"beside a $ref", `{$ref: '#/paths/~1b', put: {operationId: putA, responses: {"200": {description: OK}}}}`},
		{"an inline item", `{put: {operationId: putA, responses: {"200": {description: OK}}}}`},
	} {
		doc, diags := parseFull(t, `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a: &a `+tc.item+`
  /x: *a
  /b:
    get: {operationId: getB, responses: {"200": {description: OK}}}
`)
		openapitest.RequireNoErrorDiags(t, diags)

		assert.Equal(t, jsontext.Pointer("/paths/~1a/put"), opByPath(t, doc, "PUT", "/a").Provenance.Pointer, tc.name)
		assert.Equal(t, jsontext.Pointer("/paths/~1x/put"), opByPath(t, doc, "PUT", "/x").Provenance.Pointer,
			"%s: the alias declares at its own path", tc.name)
	}
}

// TestPathItemRefSiblings_AMergedRefIsNoReference pins the library's reading of
// a $ref a merge key supplies: none, so the item is inline, and nothing in it
// collides with itself.
func TestPathItemRefSiblings_AMergedRefIsNoReference(t *testing.T) {
	t.Parallel()
	_, diags := parseFull(t, `openapi: 3.1.0
info: {title: T, version: "1"}
x-base: &base {$ref: '#/paths/~1b'}
paths:
  /a:
    <<: *base
    put: {responses: {"200": {description: OK}}}
  /b:
    get: {responses: {"200": {description: OK}}}
`)
	assert.False(t, openapitest.HasDiag(diags, diag.PathItemRefCollision))
}

// TestPathItemRefSiblings_MalformedFieldsAreReported pins that the fields
// beside a $ref are validated as an inline item's are: a value of the wrong
// type is an error where it is written, not accepted in silence.
func TestPathItemRefSiblings_MalformedFieldsAreReported(t *testing.T) {
	t.Parallel()
	_, diags := parseFull(t, `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a:
    $ref: '#/paths/~1b'
    put:
      deprecated: notabool
      parameters: [{name: q, in: query, required: maybe, schema: {type: string}}]
      responses: {"200": {description: OK}}
  /b:
    get: {responses: {"200": {description: OK}}}
`)
	var lines []int
	for _, d := range diags {
		if d.Code == diag.Validation+"/validation-type-mismatch" {
			lines = append(lines, d.Provenance.Position.Line)
		}
	}
	slices.Sort(lines)
	assert.Equal(t, []int{7, 8}, lines, "each value of the wrong type is reported where it is written")
}

// pathItemRefOrderSpec mounts /b's operations at /a and adds its own beside the
// $ref, one of them a repeat of /b's COPY. /c writes every operation /a ends up
// with inline, in the order /b and /a write them.
const pathItemRefOrderSpec = `openapi: 3.2.0
info: {title: T, version: "1"}
paths:
  /a:
    $ref: '#/paths/~1b'
    put: {operationId: putA, responses: {"200": {description: OK}}}
    additionalOperations:
      COPY: {operationId: copyA, responses: {"200": {description: OK}}}
      LINK: {operationId: linkA, responses: {"200": {description: OK}}}
  /b:
    post: {operationId: postB, responses: {"200": {description: OK}}}
    get: {operationId: getB, responses: {"200": {description: OK}}}
    additionalOperations:
      LOCK: {operationId: lockB, responses: {"200": {description: OK}}}
      COPY: {operationId: copyB, responses: {"200": {description: OK}}}
  /c:
    post: {responses: {"200": {description: OK}}}
    put: {responses: {"200": {description: OK}}}
    get: {responses: {"200": {description: OK}}}
    additionalOperations:
      LOCK: {responses: {"200": {description: OK}}}
      COPY: {responses: {"200": {description: OK}}}
      LINK: {responses: {"200": {description: OK}}}
`

// TestPathItemRefSiblings_MountsInTheOrderAnInlineItemDoes pins the order a
// mount lists its layers' operations in: the order the same operations written
// inline get, whichever layer declares each. A layer-by-layer order put /a's
// PUT after /b's POST, and its LINK before /b's LOCK.
func TestPathItemRefSiblings_MountsInTheOrderAnInlineItemDoes(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, pathItemRefOrderSpec)
	openapitest.RequireNoErrorDiags(t, diags)

	inline := methodsAt(doc, "/c")
	require.Equal(t, []string{"GET", "PUT", "POST", "LOCK", "COPY", "LINK"}, inline)
	assert.Equal(t, inline, methodsAt(doc, "/a"), "the mount lists them as /c, written inline, does")
	assert.Equal(t, "copyA", opByPath(t, doc, "COPY", "/a").Name.Source,
		"the repeated key keeps its place and takes the nearer layer's declaration")
}

// methodsAt returns the methods of the operations mounted at uri, in the order
// the document's groups list them.
func methodsAt(doc *ir.Document, uri string) []string {
	var methods []string
	for _, g := range doc.Services[0].Groups {
		for _, op := range g.Operations {
			if hb := op.Bindings.HTTP[0]; hb.URITemplate == uri {
				methods = append(methods, hb.Method)
			}
		}
	}
	return methods
}
