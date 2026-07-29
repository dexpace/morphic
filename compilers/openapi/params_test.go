package openapi

import (
	"testing"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

func TestParams_LocationsAndSerializationDefaults(t *testing.T) {
	t.Parallel()
	spec := pathsSpec(`  /items/{id}:
    get:
      operationId: getItem
      parameters:
        - {name: id, in: path, required: true, schema: {type: string, format: uuid}}
        - {name: limit, in: query, schema: {type: integer, format: int32, default: 20}}
        - {name: filter, in: query, style: deepObject, explode: true, schema: {type: object, properties: {kind: {type: string}}}}
        - {name: X-Trace, in: header, schema: {type: string}}
        - {name: session, in: cookie, schema: {type: string}}
      responses: {"200": {description: ok}}
`)
	_, svc, diags := lowerServiceSpec(t, spec)
	requireNoErrorDiags(t, diags)
	op := firstOp(t, svc)
	require.Len(t, op.Params, 5)
	require.Len(t, op.Bindings.HTTP, 1)
	bindings := indexBy(op.Bindings.HTTP[0].ParamBindings, func(b ir.HTTPParamBinding) string { return b.Param })
	require.Len(t, bindings, 5, "every logical param bound exactly once")

	id := bindings["id"]
	assert.Equal(t, ir.HTTPLocationPath, id.Location)
	assert.Equal(t, "simple", id.Style) // resolved OpenAPI default
	require.NotNil(t, id.Explode)
	assert.False(t, *id.Explode)

	limit := bindings["limit"]
	assert.Equal(t, ir.HTTPLocationQuery, limit.Location)
	assert.Equal(t, "form", limit.Style)
	require.NotNil(t, limit.Explode)
	assert.True(t, *limit.Explode)

	assert.Equal(t, "deepObject", bindings["filter"].Style)
	assert.Equal(t, ir.HTTPLocationHeader, bindings["X-Trace"].Location)
	assert.Equal(t, ir.HTTPLocationCookie, bindings["session"].Location)

	params := indexBy(op.Params, func(p ir.Parameter) string { return p.Name.Source })
	assert.True(t, params["id"].Required, "path params are always required")
	require.NotNil(t, params["limit"].Default)
	assert.Equal(t, ir.BigVal("20"), params["limit"].Default.Num)
}

func TestParams_ContentStyleParameter(t *testing.T) {
	t.Parallel()
	spec := pathsSpec(`  /search:
    get:
      operationId: search
      parameters:
        - name: filter
          in: query
          content:
            application/json:
              schema: {type: object, properties: {kind: {type: string}}}
      responses: {"200": {description: ok}}
`)
	_, svc, diags := lowerServiceSpec(t, spec)
	requireNoErrorDiags(t, diags)
	op := firstOp(t, svc)
	require.Len(t, op.Bindings.HTTP, 1)
	require.Len(t, op.Bindings.HTTP[0].ParamBindings, 1)
	binding := op.Bindings.HTTP[0].ParamBindings[0]
	assert.Equal(t, "filter", binding.Param)
	assert.Equal(t, "application/json", binding.ContentType,
		"content-style param records its media type on the binding")
	require.Len(t, op.Params, 1)
	assert.NotEmpty(t, op.Params[0].Type.Target, "content-style param schema is lowered")
}

func TestParams_UnconvertibleExampleDiagnosed(t *testing.T) {
	t.Parallel()
	spec := pathsSpec(`  /items:
    get:
      operationId: getItem
      parameters:
        - name: q
          in: query
          schema: {type: string}
          example: !foo bar
      responses: {"200": {description: ok}}
`)
	_, svc, diags := lowerServiceSpec(t, spec)
	op := firstOp(t, svc)
	require.Len(t, op.Params, 1)
	assert.Empty(t, op.Params[0].Examples, "the unconvertible example is skipped, not appended")
	require.Equal(t, 1, countDiagsAt(diags, codeDegradedConstruct, ir.SeverityWarning))
	d, ok := firstDegradedWarning(diags)
	require.True(t, ok)
	assert.Equal(t, "/paths/~1items/get/parameters/0/example", d.Provenance.Pointer)
	assert.Contains(t, d.Message, "example:")
}

func TestParams_SchemaConstraints(t *testing.T) {
	t.Parallel()
	spec := pathsSpec(`  /people:
    get:
      operationId: listPeople
      parameters:
        - {name: age, in: query, schema: {type: integer, maximum: 120}}
      responses: {"200": {description: ok}}
`)
	_, svc, diags := lowerServiceSpec(t, spec)
	requireNoErrorDiags(t, diags)
	op := firstOp(t, svc)
	require.Len(t, op.Params, 1)
	c := op.Params[0].Constraints
	require.NotNil(t, c, "param scalar constraints land via constraintsFromSchema")
	require.NotNil(t, c.Max)
	assert.Equal(t, ir.BigVal("120"), *c.Max, "numeric bound read at full precision")
}

func TestFillParamSchema_EmptyEitherNoOp(t *testing.T) {
	t.Parallel()
	l := newRawLowerer(&soa.OpenAPI{})
	param := &ir.Parameter{}
	l.fillParamSchema(param, emptyEitherSchema(), "/p")
	assert.Nil(t, param.Constraints)
	assert.Nil(t, param.Default)
}

const paramSpec = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /search/{id}:
    parameters:
      - {name: id, in: path, required: true, schema: {type: string}}
    get:
      operationId: search
      parameters:
        - name: q
          in: query
          deprecated: true
          schema: {type: string, minLength: 1}
          examples:
            one: {value: hello}
        - name: filter
          in: query
          style: deepObject
          explode: true
          schema: {type: object}
          x-note: filtery
        - {name: X-Tok, in: header, schema: {type: string}}
        - {name: sid, in: cookie, schema: {type: string}}
        - name: complex
          in: query
          content:
            application/json:
              schema: {type: object, properties: {a: {type: string}}}
        - name: bad
          in: query
          schema: {type: number, default: .inf, minimum: .inf}
        - {name: bare, in: query}
      responses:
        "200": {description: ok}
`

func TestParams_AllLocationsAndStyles(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, paramSpec)
	op := findOp(t, doc, "search")
	byName := indexBy(op.Bindings.HTTP[0].ParamBindings, func(b ir.HTTPParamBinding) string { return b.Param })
	assert.Equal(t, ir.HTTPLocationPath, byName["id"].Location)
	assert.Equal(t, ir.HTTPLocationQuery, byName["q"].Location)
	assert.Equal(t, ir.HTTPLocationHeader, byName["X-Tok"].Location)
	assert.Equal(t, ir.HTTPLocationCookie, byName["sid"].Location)
	assert.Equal(t, "deepObject", byName["filter"].Style)
	assert.Equal(t, "application/json", byName["complex"].ContentType)

	logical := indexBy(op.Params, func(p ir.Parameter) string { return p.Name.Source })
	assert.True(t, logical["id"].Required, "path param always required")
	require.NotNil(t, logical["q"].Deprecation)
	assert.NotEmpty(t, logical["q"].Examples)
	assert.NotEmpty(t, logical["filter"].Unmodeled)
	require.NotNil(t, logical["q"].Constraints)

	assert.True(t, hasDiagAt(diags, codeDegradedConstruct, ir.SeverityWarning), "malformed param default warns")
	assert.True(t, hasDiag(diags, codeNumericPrecision), "malformed param constraint warns")
}

func TestParams_QueryStringLocation(t *testing.T) {
	t.Parallel()
	spec := pathsSpecVer("3.2.0", `  /q:
    get:
      operationId: q
      parameters:
        - {name: qs, in: querystring, schema: {type: string}}
      responses:
        "200": {description: ok}
`)
	doc, _ := parseFull(t, spec)
	op := findOp(t, doc, "q")
	assert.Equal(t, ir.HTTPLocationQuerystring, op.Bindings.HTTP[0].ParamBindings[0].Location)
}

const componentParamRefSpec = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a:
    get:
      operationId: getA
      parameters:
        - {$ref: '#/components/parameters/Page'}
      responses: {"200": {description: ok}}
  /b:
    get:
      operationId: getB
      parameters:
        - {$ref: '#/components/parameters/Page'}
      responses: {"200": {description: ok}}
components:
  parameters:
    Page: {name: page, in: query, schema: {type: string, enum: [a, b]}}
`

// TestParams_ComponentRefSharedAcrossOperationsInternsOnce is the fix's core
// scenario for a referenced parameter component (issue #107): two operations
// $ref'ing one #/components/parameters/Page must intern its schema once, at
// the component's own declaration pointer, rather than once per fabricated
// use-site position.
func TestParams_ComponentRefSharedAcrossOperationsInternsOnce(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, componentParamRefSpec)
	requireNoErrorDiags(t, diags)
	getA := findOp(t, doc, "getA")
	getB := findOp(t, doc, "getB")
	require.Len(t, getA.Params, 1)
	require.Len(t, getB.Params, 1)

	wantID := ir.TypeID("t/anon/components/parameters/Page/schema")
	assert.Equal(t, wantID, getA.Params[0].Type.Target)
	assert.Equal(t, wantID, getB.Params[0].Type.Target, "both operations resolve the same shared schema")
	_, ok := doc.Types[wantID]
	require.True(t, ok, "the shared schema is registered under the component's own pointer")

	_, fabricatedA := doc.Types[ir.TypeID("t/anon/paths/~1a/get/parameters/0/schema")]
	_, fabricatedB := doc.Types[ir.TypeID("t/anon/paths/~1b/get/parameters/0/schema")]
	assert.False(t, fabricatedA, "no fabricated per-operation ID for /a")
	assert.False(t, fabricatedB, "no fabricated per-operation ID for /b")
}

const componentContentParamRefSpec = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a:
    get:
      operationId: getA
      parameters:
        - {$ref: '#/components/parameters/Filter'}
      responses: {"200": {description: ok}}
  /b:
    get:
      operationId: getB
      parameters:
        - {$ref: '#/components/parameters/Filter'}
      responses: {"200": {description: ok}}
components:
  parameters:
    Filter:
      name: filter
      in: query
      content:
        application/json:
          schema: {type: object, properties: {q: {type: string}}}
`

// TestParams_ContentStyleComponentRefInternsOnce covers fillParamType's other
// branch: a content-style parameter hoists under <param>/content/<mt>/schema
// rather than <param>/schema, so it needs its own guard that the base pointer
// is the component's declaration and not the referencing operation (issue #107).
func TestParams_ContentStyleComponentRefInternsOnce(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, componentContentParamRefSpec)
	requireNoErrorDiags(t, diags)
	getA := findOp(t, doc, "getA")
	getB := findOp(t, doc, "getB")
	require.Len(t, getA.Params, 1)
	require.Len(t, getB.Params, 1)

	wantID := ir.TypeID("t/anon/components/parameters/Filter/content/application~1json/schema")
	assert.Equal(t, wantID, getA.Params[0].Type.Target)
	assert.Equal(t, wantID, getB.Params[0].Type.Target, "both operations share the content parameter's schema")
	_, ok := doc.Types[wantID]
	require.True(t, ok, "the schema is registered under the component's own pointer")
	assert.Equal(t, "application/json", getA.Bindings.HTTP[0].ParamBindings[0].ContentType)
}

// TestParams_SchemaAnnotationsReachTheParameter asserts a parameter schema's
// annotations land on the ir.Parameter that carries the position. Only its
// constraints used to survive; the rest were dropped with no diagnostic even
// though Parameter has Docs, Examples and Unmodeled (GitHub #116).
func TestParams_SchemaAnnotationsReachTheParameter(t *testing.T) {
	t.Parallel()
	_, svc, diags := lowerServiceSpec(t, pathsSpec(
		"  /x:\n    get:\n      operationId: g\n      parameters:\n"+
			"        - name: q\n          in: query\n          schema: "+inlineProbeBody+"\n"+
			"      responses: {\"204\": {description: ok}}\n"))
	requireNoErrorDiags(t, diags)

	p := firstOp(t, svc).Params[0]
	assertProbeDocsKept(t, p.Docs)
	assert.NotNil(t, p.Deprecation)
	assertProbeExample(t, p.Examples)
	assert.Contains(t, p.Unmodeled, "openapi:x-vendor")
	assert.Contains(t, p.Unmodeled, "openapi:not")
	require.NotNil(t, p.Constraints)
	require.NotNil(t, p.Constraints.MaxLength)
	assert.Equal(t, int64(3), *p.Constraints.MaxLength)
}

// TestParams_SchemaAnnotationsSurviveARefNamingTheSchema pins the parameter
// half of the pointer collision. A $ref can name a parameter's schema pointer,
// which hoists a node there — and components lower before paths, so that node
// always exists by the time the parameter is reached. Reading it as the
// declaration's home cost every parameter in this shape all of its schema
// annotations, in the only order there is.
func TestParams_SchemaAnnotationsSurviveARefNamingTheSchema(t *testing.T) {
	t.Parallel()
	doc, svc, diags := lowerServiceSpec(t,
		"openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths:\n"+
			"  /x:\n    get:\n      operationId: g\n      parameters:\n"+
			"        - name: q\n          in: query\n          schema: "+inlineProbeBody+"\n"+
			"      responses: {\"204\": {description: ok}}\n"+
			"components:\n  schemas:\n"+
			"    Outsider: {$ref: '#/paths/~1x/get/parameters/0/schema'}\n")
	requireNoErrorDiags(t, diags)

	p := firstOp(t, svc).Params[0]
	assert.Equal(t, ir.TypeID("t/prim/string"), p.Type.Target,
		"the parameter's own type is unchanged by the outside reference")
	assertProbeDocsKept(t, p.Docs)
	assert.NotNil(t, p.Deprecation)
	assert.Contains(t, p.Unmodeled, "openapi:x-vendor")
	assert.Contains(t, p.Unmodeled, "openapi:not")

	sc, ok := doc.Types["t/anon/paths/~1x/get/parameters/0/schema"].(*ir.Scalar)
	require.True(t, ok, "and the referenced pointer still names the schema written there")
	assertProbeDocsKept(t, sc.Docs)
}

// paramRefInheritSpec gives every parameter the same referent and varies only
// what each writes beside its own $ref: Q declares one of each inheritable
// keyword plus a constraint, and Hop is a bare hop on the way to it.
const paramRefInheritSpec = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /x:
    get:
      operationId: g
      parameters:
        - {name: bare, in: query, schema: {$ref: '#/components/schemas/Q'}}
        - name: override
          in: query
          schema:
            $ref: '#/components/schemas/Q'
            description: USE_SITE
            default: use-site
            deprecated: false
        - {name: chained, in: query, schema: {$ref: '#/components/schemas/Hop'}}
      responses: {"200": {description: ok}}
components:
  schemas:
    Q: {type: string, description: REFERENT, deprecated: true, default: referent, maxLength: 9}
    Hop: {$ref: '#/components/schemas/Q'}
`

// paramsOf indexes an operation's parameters by source name.
func paramsOf(t *testing.T, svc ir.Service) map[string]ir.Parameter {
	t.Helper()
	return indexBy(firstOp(t, svc).Params, func(p ir.Parameter) string { return p.Name.Source })
}

// TestParams_RefSchemaInheritsFromItsReferent covers the referent-fallback half
// of the $ref rule at a parameter (GitHub #131). A parameter whose schema is a
// bare $ref used to carry nothing at all — no description, no deprecation, no
// default — because the position never resolved the target it was holding.
func TestParams_RefSchemaInheritsFromItsReferent(t *testing.T) {
	t.Parallel()
	_, svc, diags := lowerServiceSpec(t, paramRefInheritSpec)
	requireNoErrorDiags(t, diags)

	p := paramsOf(t, svc)["bare"]
	assert.Equal(t, "REFERENT", p.Docs.Description, "the referent's description reaches the parameter")
	assert.NotNil(t, p.Deprecation, "and its deprecation")
	require.NotNil(t, p.Default, "and its default")
	assert.Equal(t, "referent", p.Default.Str)
	assert.Nil(t, p.Constraints,
		"but never its constraints: fillPropertyConstraints keeps those use-site-only too")
}

// TestParams_RefSchemaUseSiteWinsOverItsReferent pins the half that already
// worked, so closing the fallback cannot quietly invert the precedence: what is
// written beside the $ref describes this use of the type and outranks the target.
func TestParams_RefSchemaUseSiteWinsOverItsReferent(t *testing.T) {
	t.Parallel()
	_, svc, diags := lowerServiceSpec(t, paramRefInheritSpec)
	requireNoErrorDiags(t, diags)

	p := paramsOf(t, svc)["override"]
	assert.Equal(t, "USE_SITE", p.Docs.Description, "the use-site description wins")
	require.NotNil(t, p.Default)
	assert.Equal(t, "use-site", p.Default.Str, "and the use-site default")
	assert.Nil(t, p.Deprecation, "and an explicit deprecated: false suppresses the referent's true")
}

// TestParams_RefSchemaInheritsThroughARefChain pins which resolution the
// fallback must use. Hop writes nothing but its own $ref, so a one-hop referent
// (siteAt's) would leave the parameter with nothing; only following the chain to
// its end reaches Q.
func TestParams_RefSchemaInheritsThroughARefChain(t *testing.T) {
	t.Parallel()
	_, svc, diags := lowerServiceSpec(t, paramRefInheritSpec)
	requireNoErrorDiags(t, diags)

	p := paramsOf(t, svc)["chained"]
	assert.Equal(t, "REFERENT", p.Docs.Description, "one hop reaches Hop, which declares nothing")
	assert.NotNil(t, p.Deprecation)
	require.NotNil(t, p.Default)
	assert.Equal(t, "referent", p.Default.Str)
}

const paramXMLSpec = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /x:
    get:
      operationId: g
      parameters:
        - {name: q, in: query, schema: {type: string, xml: {name: XQ}}}
        - name: c
          in: query
          content:
            application/xml:
              schema: {type: string, xml: {name: XCS}}
      responses: {"200": {description: ok}}
`

// TestParams_SchemaXMLHintsKeptUnderUnmodeled covers the one annotation
// ir.Parameter has no field for (GitHub #124), which used to be dropped with no
// diagnostic at all. It is not inert here: the content-style case binds
// application/xml, the media type OpenAPI §4.8.26 conditions xml on, and the
// binding records that content type.
func TestParams_SchemaXMLHintsKeptUnderUnmodeled(t *testing.T) {
	t.Parallel()
	_, svc, diags := lowerServiceSpec(t, paramXMLSpec)
	requireNoErrorDiags(t, diags)
	params := paramsOf(t, svc)

	cases := []struct {
		param, schemaPtr, want string
	}{
		{"q", "/paths/~1x/get/parameters/0/schema", `{"name":"XQ"}`},
		{"c", "/paths/~1x/get/parameters/1/content/application~1xml/schema", `{"name":"XCS"}`},
	}
	for _, tc := range cases {
		t.Run(tc.param, func(t *testing.T) {
			t.Parallel()
			entry, ok := params[tc.param].Unmodeled["openapi:xml"]
			require.True(t, ok, "an xml hint with no Parameter field is kept raw, not dropped")
			assert.Equal(t, ir.ReasonNoIRHome, entry.Reason)
			assert.JSONEq(t, tc.want, string(entry.Value), "kept verbatim")
			assert.Equal(t, tc.schemaPtr+"/xml", entry.Provenance.Pointer,
				"located at the xml keyword itself")
			assert.Contains(t,
				diagMessageAt(t, diags, codeDegradedConstruct, ir.SeverityInfo, tc.schemaPtr),
				"xml hints", "and announced once")
		})
	}
}

// TestParams_SchemaXMLHintsStayOnAnOwnedNode is the other half of one home per
// declaration: a parameter schema that hoisted a node of its own already has a
// structural XML field, so preserving a second copy on the parameter would give
// one declaration two homes that can drift.
func TestParams_SchemaXMLHintsStayOnAnOwnedNode(t *testing.T) {
	t.Parallel()
	doc, svc, diags := lowerServiceSpec(t, pathsSpec(
		"  /x:\n    get:\n      operationId: g\n      parameters:\n"+
			"        - {name: obj, in: query, schema: {type: object, xml: {name: XOBJ}}}\n"+
			"      responses: {\"204\": {description: ok}}\n"))
	requireNoErrorDiags(t, diags)

	p := paramsOf(t, svc)["obj"]
	node, ok := doc.Types[p.Type.Target]
	require.True(t, ok, "the object schema owns a node of its own")
	require.NotNil(t, node.Common().XML)
	assert.Equal(t, "XOBJ", node.Common().XML.Name)
	assert.NotContains(t, p.Unmodeled, "openapi:xml", "so the parameter keeps no second copy")
	assert.Equal(t, 0, countDiagsAt(diags, codeDegradedConstruct, ir.SeverityInfo),
		"and nothing is announced as homeless")
}

// TestParams_OwnAnnotationsWinOverTheSchema checks the precedence a parameter
// shares with a header: the parameter object describes this input, its schema
// describes the type, and the more specific of the two wins.
func TestParams_OwnAnnotationsWinOverTheSchema(t *testing.T) {
	t.Parallel()
	_, svc, diags := lowerServiceSpec(t, pathsSpec(
		"  /x:\n    get:\n      operationId: g\n      parameters:\n"+
			"        - name: q\n          in: query\n          description: PARAM\n"+
			"          x-scope: param\n          schema: {type: string, description: SCHEMA, x-scope: schema}\n"+
			"      responses: {\"204\": {description: ok}}\n"))
	requireNoErrorDiags(t, diags)

	p := firstOp(t, svc).Params[0]
	assert.Equal(t, "PARAM", p.Docs.Description, "the parameter's own description wins")
	raw, ok := p.Unmodeled["openapi:x-scope"]
	require.True(t, ok)
	assert.JSONEq(t, `"param"`, string(raw.Value), "and its own extension overlays the schema's")
}
