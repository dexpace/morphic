package operation_test

import (
	"encoding/json/jsontext"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

func TestOperation_ExplicitlyPublicSecurity(t *testing.T) {
	t.Parallel()
	spec := openapitest.PathsSpec(`  /open:
    get:
      operationId: open
      security: []
      responses: {"200": {description: ok}}
  /inherits:
    get:
      operationId: inherits
      responses: {"200": {description: ok}}
`)
	_, svc, diags := compileService(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
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

func TestOperation_NoOperationIdHint(t *testing.T) {
	t.Parallel()
	spec := openapitest.PathsSpec(`  /ping:
    get:
      responses: {"200": {description: ok}}
`)
	_, svc, diags := compileService(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	op := openapitest.FirstOp(t, svc)
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

	getA := openapitest.FindOp(t, doc, "getA")
	assert.NotEmpty(t, getA.Unmodeled, "op x-* extension")
	assert.NotEmpty(t, getA.Docs.ExternalDocs, "op externalDocs")
	assert.Len(t, getA.Servers, 1, "the path item's servers are named on its operation")
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
	assert.NotEmpty(t, openapitest.FindOp(t, doc, "cbPost").ID)
	_ = diags
}

func TestOperations_NoResponses(t *testing.T) {
	t.Parallel()
	doc, _ := parseFull(t, opsSpec)
	trace := openapitest.FindOp(t, doc, "traceA")
	assert.Empty(t, trace.Responses)
	assert.Empty(t, trace.Errors)
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
	openapitest.RequireNoErrorDiags(t, diags)

	var root yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte(provenanceSpec), &root))
	check := func(kind, name string, pointer jsontext.Pointer) int {
		if pointer == "" {
			return 0
		}
		assert.True(t, pointerResolves(&root, string(pointer)),
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

// headersOf collects an operation's Headers from both places they live:
// response headers, and the per-part headers of a multipart encoding.
func headersOf(op ir.Operation) []ir.Header {
	var out []ir.Header
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
// node in a parsed YAML document: each token (unescaped via jsontext.Pointer's
// own Tokens) is followed as a mapping key, or, in a sequence, a decimal index.
// The empty pointer names the whole document and resolves; callers skip it.
func pointerResolves(root *yaml.Node, pointer string) bool {
	node := root
	if node.Kind == yaml.DocumentNode && len(node.Content) > 0 {
		node = node.Content[0]
	}
	for token := range jsontext.Pointer(pointer).Tokens() {
		next, ok := pointerStep(node, token)
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

const sharedDefectiveBodySpec = `openapi: 3.1.0
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
          schema: {type: string, required: [n]}
  responses:
    Err:
      description: err
      headers:
        X-E: {schema: {type: string, required: [n]}}
      content:
        application/json:
          schema: {type: object}
`

// TestDiag_SharedDeclarationReportsEachDefectOnce pins that lowering a
// referenced component at its declaration reports each defect once. Both
// operations reach the same request body, whose scalar schema carries a
// `required` the lowered node has no field for, and the same error response,
// whose header's scalar schema carries one too. Reported per use site, a
// component shared by twenty operations would repeat each line twenty times.
//
// The second defect sits on an error response's header on purpose, so the case
// covers the shared-declaration rule on the path GitHub #422 added, not only on
// the success side.
func TestDiag_SharedDeclarationReportsEachDefectOnce(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, sharedDefectiveBodySpec)

	seen := map[string]int{}
	for _, d := range diags {
		seen[string(d.Severity)+"|"+d.Code+"|"+string(d.Provenance.Pointer)+"|"+d.Message]++
	}
	for key, n := range seen {
		assert.Equal(t, 1, n, "one defect, one diagnostic: %s", key)
	}

	// Every defect still surfaces — de-duplication must not silence any of them.
	assert.Equal(t, 2, openapitest.CountDiagsAt(diags, diag.DegradedConstruct, ir.SeverityInfo),
		"the body schema's homeless required and the error header's are two distinct defects")

	// The shared component's own `required: false` reaches both use sites, as
	// two values rather than one aliased pointer: lowering it at its declaration
	// de-duplicates the diagnostics, not the field.
	for _, name := range []string{"postA", "postB"} {
		op := openapitest.FindOp(t, doc, name)
		require.NotNil(t, op.Request, "%s has a body", name)
		require.NotNil(t, op.Request.Required, "%s: OpenAPI states body optionality", name)
		assert.False(t, *op.Request.Required, "%s: the component declares required: false", name)
	}
	assert.NotSame(t, openapitest.FindOp(t, doc, "postA").Request.Required,
		openapitest.FindOp(t, doc, "postB").Request.Required,
		"each use site owns its flag, so an emitter mutating one cannot reach the other")
}

// TestDiag_DistinctDefectsAtOnePointerBothSurvive is the control for the rule
// above: de-duplication compares the whole diagnostic, so two different messages
// at one pointer are two findings, not one repeated.
func TestDiag_DistinctDefectsAtOnePointerBothSurvive(t *testing.T) {
	t.Parallel()
	var c lowering.Ctx
	var acc compile.Diags
	acc.Append(c.DiagAt(ir.SeverityWarning, diag.DegradedConstruct, "/p", "first"))
	acc.Append(c.DiagAt(ir.SeverityWarning, diag.DegradedConstruct, "/p", "second"))
	acc.Append(c.DiagAt(ir.SeverityWarning, diag.DegradedConstruct, "/p", "first"))
	require.Len(t, acc.List(), 2, "the repeat is dropped, the distinct message is not")
	assert.Equal(t, "first", acc.List()[0].Message)
	assert.Equal(t, "second", acc.List()[1].Message)
}

// TestOperation_UnserializableExtensionStillWarns pins the operation's half of
// what TestAuth_UnserializableExtensionStillWarns pins for a security scheme: an
// x-* extension that cannot be serialized is announced even though nothing of it
// is kept, so the operation's own Unmodeled ends up empty.
//
// The warning and the entry travel separately, and only the entry is gated on
// being non-empty. Recording one without the other is the shape this catches —
// an operation that quietly loses an extension it could not represent.
func TestOperation_UnserializableExtensionStillWarns(t *testing.T) {
	t.Parallel()
	spec := openapitest.PathsSpec(`  /x:
    get:
      x-bad: {n: .nan}
      responses: {"200": {description: ok}}
`)
	doc, diags := parseFull(t, spec)

	var announced bool
	for _, d := range diags {
		if d.Code == diag.DegradedConstruct && d.Severity == ir.SeverityWarning &&
			strings.Contains(d.Message, "x-bad") {
			announced = true
		}
	}
	assert.True(t, announced,
		"the unserializable extension is named in a warning, not merely some warning: %+v", diags)
	op := doc.Services[0].Groups[0].Operations[0]
	assert.Empty(t, op.Unmodeled, "and is dropped rather than stored half-converted")
}

// ghostRefsSpec references non-existent components everywhere so every
// resolve-or-skip path (unresolved GetObject → nil) and resolution-error branch
// is exercised without a panic.
const ghostRefsSpec = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a:
    parameters:
      - {$ref: '#/components/parameters/GhostParam'}
    get:
      operationId: getA
      callbacks:
        good:
          '{$url}': {$ref: '#/components/pathItems/GhostInner'}
        bad: {$ref: '#/components/callbacks/GhostCb'}
      requestBody: {$ref: '#/components/requestBodies/GhostBody'}
      responses:
        "200": {$ref: '#/components/responses/GhostResp'}
        "201":
          description: ok
          headers:
            X-H: {$ref: '#/components/headers/GhostHeader'}
          content:
            application/json:
              schema: {type: string}
              examples:
                one: {$ref: '#/components/examples/GhostEx'}
  /ref: {$ref: '#/components/pathItems/GhostItem'}
webhooks:
  hook: {$ref: '#/components/pathItems/GhostHook'}
`

// TestGhostRefs_AllResolversDegradeGracefully drives every reference-or-inline
// entry the operation walk resolves against a component that does not exist, so
// each resolve-or-skip branch is reached in one pass.
//
// It holds what the skips do, not only that nothing panics. Each of them decides
// whether a construct the document does not actually declare ends up in the IR.
func TestGhostRefs_AllResolversDegradeGracefully(t *testing.T) {
	t.Parallel()
	// A reference that resolves to nothing is a diagnostic, not a parse failure,
	// so the whole spec compiles and every skip is reached in one pass.
	doc, diags := parseFull(t, ghostRefsSpec)
	require.NotEmpty(t, diags, "unresolved refs reported")

	// And the skips themselves are silent: every diagnostic here is the load
	// phase's report of a $ref it followed to nothing, at the $ref itself
	// (GitHub #385). A skip lowering an empty stand-in instead would report on a
	// construct the document never wrote, beside the one report per $ref below.
	wantRefs := map[string]string{
		"/paths/~1a/parameters/0":                                             "#/components/parameters/GhostParam",
		"/paths/~1a/get/callbacks/good/{$url}":                                "#/components/pathItems/GhostInner",
		"/paths/~1a/get/callbacks/bad":                                        "#/components/callbacks/GhostCb",
		"/paths/~1a/get/requestBody":                                          "#/components/requestBodies/GhostBody",
		"/paths/~1a/get/responses/200":                                        "#/components/responses/GhostResp",
		"/paths/~1a/get/responses/201/headers/X-H":                            "#/components/headers/GhostHeader",
		"/paths/~1a/get/responses/201/content/application~1json/examples/one": "#/components/examples/GhostEx",
		"/paths/~1ref":                                                        "#/components/pathItems/GhostItem",
		"/webhooks/hook":                                                      "#/components/pathItems/GhostHook",
	}
	require.Len(t, diags, len(wantRefs),
		"one report per ghost reference, not a second added by a lowering resolver: %+v", diags)
	for pointer, ref := range wantRefs {
		msg := openapitest.DiagMessageAt(t, diags, diag.UnresolvedRef, ir.SeverityError, pointer)
		assert.Contains(t, msg, fmt.Sprintf("unresolved $ref %q", ref), "names the reference that failed")
	}

	// What each skip has to do is contribute nothing — not an empty stand-in.
	// Asserting only "no panic" leaves that unheld: lowering an unresolvable path
	// item as an empty one, rather than skipping it, passes a no-panic test and
	// every other test in this repo, while putting a path the document does not
	// declare into the IR.
	require.Len(t, doc.Services[0].Groups, 1, "the ghost webhook adds no group of its own")
	ops := doc.Services[0].Groups[0].Operations
	require.Len(t, ops, 1,
		"only the one declared operation survives: the ghost path item, the ghost webhook, the "+
			"ghost callback and the callback's ghost path item each contribute none")
	op := ops[0]
	assert.Equal(t, "getA", op.Name.Source)
	assert.Empty(t, op.Params, "the ghost parameter is skipped, not lowered as a nameless one")
	assert.Nil(t, op.Request, "as is the ghost request body")

	require.Len(t, op.Responses, 1, "the ghost response is skipped; the inline 201 is not")
	resp := op.Responses[0]
	assert.Empty(t, resp.Headers, "the ghost header is skipped")
	require.NotNil(t, resp.Payload)
	require.Len(t, resp.Payload.Contents, 1)
	assert.Empty(t, resp.Payload.Contents[0].Examples, "and the ghost example")
}

// TestOperation_DeprecatedIsCarried pins the one flag an operation can raise
// about itself. It is a declared fact, not an inference, so it lands on the
// operation rather than in Unmodeled — and an SDK that hides deprecated calls
// has nothing else to read.
func TestOperation_DeprecatedIsCarried(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.PathsSpec(`  /a:
    get: {operationId: getA, deprecated: true, responses: {"200": {description: ok}}}
    post: {operationId: postA, responses: {"200": {description: ok}}}
`))
	openapitest.RequireNoErrorDiags(t, diags)

	assert.NotNil(t, openapitest.FindOp(t, doc, "getA").Deprecation, "the declared flag is carried")
	assert.Nil(t, openapitest.FindOp(t, doc, "postA").Deprecation, "and an operation that declares none has none")
}

// operationServersSpec declares `servers` at both levels OpenAPI allows, on an
// operation of each of the three routes that lower one, with a distinct URL
// everywhere so a preserved list can be traced to the object that wrote it. The
// last two path items cover the single-level shapes.
const operationServersSpec = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /both:
    servers: [{url: 'https://pathitem.example'}]
    post:
      operationId: postBoth
      servers: [{url: 'https://operation.example'}]
      callbacks:
        onEvent:
          '{$request.body#/url}':
            servers: [{url: 'https://cb-pathitem.example'}]
            post:
              operationId: onEvent
              servers: [{url: 'https://cb-operation.example'}]
              responses: {"200": {description: ok}}
      responses: {"200": {description: ok}}
  /operationOnly:
    get:
      operationId: getOperationOnly
      servers: [{url: 'https://only.example'}]
      responses: {"200": {description: ok}}
  /pathItemOnly:
    servers: [{url: 'https://pathonly.example'}]
    get:
      operationId: getPathItemOnly
      responses: {"200": {description: ok}}
webhooks:
  hooked:
    servers: [{url: 'https://wh-pathitem.example'}]
    post:
      operationId: onHook
      servers: [{url: 'https://wh-operation.example'}]
      responses: {"200": {description: ok}}
`

// serversNamed returns the URL of each server ids names, in order, read from
// doc's registry, failing when one names nothing there.
func serversNamed(t *testing.T, doc *ir.Document, ids []ir.ServerID) []string {
	t.Helper()
	urls := make([]string, 0, len(ids))
	for _, id := range ids {
		s, ok := doc.Servers[id]
		require.True(t, ok, "%s names no registered server", id)
		urls = append(urls, s.URLTemplate)
	}
	return urls
}

// TestOperations_OwnServersOverrideThePathItems pins the precedence OpenAPI
// gives the two levels, on every route that lowers an operation: an operation
// is sent to its own servers, which override its path item's. The path item's
// are registered all the same, at their own pointer, so the override loses no
// declaration; and neither level is reported, since each has its home.
//
// All three routes are asserted because a single-route case cannot see a fix
// that skips the other two (GitHub #39).
func TestOperations_OwnServersOverrideThePathItems(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, operationServersSpec)
	openapitest.RequireNoErrorDiags(t, diags)
	assert.False(t, openapitest.HasDiag(diags, diag.DegradedConstruct), "no servers list is degraded: %+v", diags)

	for _, tc := range []struct{ op, own, ownAt, overridden, overriddenAt string }{
		{"postBoth",
			"https://operation.example", "/paths/~1both/post/servers/0",
			"https://pathitem.example", "/paths/~1both/servers/0"},
		{"onHook",
			"https://wh-operation.example", "/webhooks/hooked/post/servers/0",
			"https://wh-pathitem.example", "/webhooks/hooked/servers/0"},
		{"onEvent",
			"https://cb-operation.example",
			"/paths/~1both/post/callbacks/onEvent/{$request.body#~1url}/post/servers/0",
			"https://cb-pathitem.example",
			"/paths/~1both/post/callbacks/onEvent/{$request.body#~1url}/servers/0"},
	} {
		op := openapitest.FindOp(t, doc, tc.op)
		assert.Equal(t, []string{tc.own}, serversNamed(t, doc, op.Servers),
			"%s is sent to its own servers, not its path item's", tc.op)
		assert.Equal(t, jsontext.Pointer(tc.ownAt), doc.Servers[op.Servers[0]].Provenance.Pointer)

		overridden := ir.ServerID("server/openapi" + tc.overriddenAt)
		require.Contains(t, doc.Servers, overridden, "%s's path item list is registered though overridden", tc.op)
		assert.Equal(t, tc.overridden, doc.Servers[overridden].URLTemplate)
		assert.Equal(t, jsontext.Pointer(tc.overriddenAt), doc.Servers[overridden].Provenance.Pointer,
			"each server keeps the coordinate of the object that declared it")
		assert.NotContains(t, op.Unmodeled, "openapi:servers", "neither level is kept verbatim")
		assert.NotContains(t, op.Unmodeled, "openapi:operationServers")
	}
}

// TestOperations_ServersEachLevelAlone is the control for the test above: an
// operation declaring one level is sent to that level's servers, and one whose
// path item and itself declare none names none, so its service's apply.
func TestOperations_ServersEachLevelAlone(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.PathsSpec(`  /operationOnly:
    get:
      operationId: getOperationOnly
      servers: [{url: 'https://only.example'}]
      responses: {"200": {description: ok}}
  /pathItemOnly:
    servers: [{url: 'https://pathonly.example'}]
    get:
      operationId: getPathItemOnly
      responses: {"200": {description: ok}}
  /neither:
    get:
      operationId: getNeither
      responses: {"200": {description: ok}}
`))
	openapitest.RequireNoErrorDiags(t, diags)

	assert.Equal(t, []string{"https://only.example"},
		serversNamed(t, doc, openapitest.FindOp(t, doc, "getOperationOnly").Servers))
	assert.Equal(t, []string{"https://pathonly.example"},
		serversNamed(t, doc, openapitest.FindOp(t, doc, "getPathItemOnly").Servers),
		"an operation declaring none of its own is sent to its path item's")
	assert.Nil(t, openapitest.FindOp(t, doc, "getNeither").Servers,
		"an operation neither level scopes names none, so its service's apply")
	assert.Equal(t, []string{"/"}, serversNamed(t, doc, doc.Services[0].Servers),
		"and the service is served at the root OpenAPI supplies for a document declaring none")
}

// TestOperations_PathItemServersAreEachOperationsOwn pins that the operations of
// one path item hold a list each, so a pass changing one's leaves the others'.
func TestOperations_PathItemServersAreEachOperationsOwn(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.PathsSpec(`  /x:
    servers: [{url: 'https://x.example'}]
    get: {operationId: getX, responses: {"200": {description: ok}}}
    put: {operationId: putX, responses: {"200": {description: ok}}}
`))
	openapitest.RequireNoErrorDiags(t, diags)
	get, put := openapitest.FindOp(t, doc, "getX"), openapitest.FindOp(t, doc, "putX")
	require.Equal(t, []ir.ServerID{"server/openapi/paths/~1x/servers/0"}, put.Servers)
	require.Equal(t, put.Servers, get.Servers, "both name the one declaration")

	get.Servers[0] = "server/elsewhere"
	assert.Equal(t, ir.ServerID("server/openapi/paths/~1x/servers/0"), put.Servers[0])
}

// TestOperations_OwnServerKeepsAndReportsAnUndeclaredKey pins that what an
// operation's own Server Object writes outside the object lands on the server,
// and is reported at the key, as a document's server's is.
func TestOperations_OwnServerKeepsAndReportsAnUndeclaredKey(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.PathsSpec(`  /x:
    get:
      operationId: getX
      servers: [{url: 'https://x.example', stray: 1}]
      responses: {"200": {description: ok}}
`))
	openapitest.RequireNoErrorDiags(t, diags)

	const own = ir.ServerID("server/openapi/paths/~1x/get/servers/0")
	assert.Contains(t, doc.Servers[own].Unmodeled, "openapi:stray")
	assert.True(t, openapitest.HasDiagCodeAt(diags, diag.UnknownObjectKey, "/paths/~1x/get/servers/0/stray"))
}

// TestOperations_OwnServersSurviveBesideExtensions pins that an operation's own
// servers and its extensions both land when one operation declares both: the
// servers on Servers, the extension in Unmodeled, neither written over.
func TestOperations_OwnServersSurviveBesideExtensions(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.PathsSpec(`  /x:
    get:
      operationId: getX
      servers: [{url: 'https://operation.example'}]
      x-vendor: kept
      responses: {"200": {description: ok}}
`))
	openapitest.RequireNoErrorDiags(t, diags)

	op := openapitest.FindOp(t, doc, "getX")
	assert.Equal(t, []string{"https://operation.example"}, serversNamed(t, doc, op.Servers))
	assert.Contains(t, op.Unmodeled, "openapi:x-vendor", "and the extensions survive beside them")
}

// TestLowerService_PercentEncodedEntryRefKeepsTheDeclaredCoordinate covers the
// same identity defect on the components that are not schemas, where it was
// wholly silent. Their entries resolve through the library, so the value arrived
// intact and no unresolved-ref was ever emitted; only the pointer the entry
// carried stayed encoded, and every ID hoisted beneath it inherited the
// encoding — here the response's own content schema.
func TestLowerService_PercentEncodedEntryRefKeepsTheDeclaredCoordinate(t *testing.T) {
	t.Parallel()
	doc, _, diags := compileService(t, `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a:
    get:
      responses:
        '200': {$ref: '#/components/responses/My%2DResp'}
components:
  responses:
    My-Resp:
      description: ok
      content:
        application/json:
          schema: {type: object, properties: {q: {type: string}}}
`)
	openapitest.RequireNoErrorDiags(t, diags)

	const want = ir.TypeID("t/anon/components/responses/My-Resp/content/application~1json/schema")
	assert.Contains(t, doc.Types, want,
		"the response body is hoisted at the coordinate the component declares")
	for id := range doc.Types {
		assert.NotContains(t, string(id), "%",
			"no node is interned at an encoded path")
	}
}
