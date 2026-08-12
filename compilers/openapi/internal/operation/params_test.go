package operation_test

import (
	"strings"
	"testing"

	"github.com/speakeasy-api/openapi/validation"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

func TestParams_LocationsAndSerializationDefaults(t *testing.T) {
	t.Parallel()
	spec := openapitest.PathsSpec(`  /items/{id}:
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
	openapitest.RequireNoErrorDiags(t, diags)
	op := openapitest.FirstOp(t, svc)
	require.Len(t, op.Params, 5)
	require.Len(t, op.Bindings.HTTP, 1)
	bindings := openapitest.IndexBy(op.Bindings.HTTP[0].ParamBindings, func(b ir.HTTPParamBinding) string { return b.Param })
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

	params := openapitest.IndexBy(op.Params, func(p ir.Parameter) string { return p.Name.Source })
	assert.True(t, params["id"].Required, "path params are always required")
	require.NotNil(t, params["limit"].Default)
	assert.Equal(t, ir.BigVal("20"), params["limit"].Default.Num)
}

func TestParams_ContentStyleParameter(t *testing.T) {
	t.Parallel()
	spec := openapitest.PathsSpec(`  /search:
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
	openapitest.RequireNoErrorDiags(t, diags)
	op := openapitest.FirstOp(t, svc)
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
	spec := openapitest.PathsSpec(`  /items:
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
	op := openapitest.FirstOp(t, svc)
	require.Len(t, op.Params, 1)
	assert.Empty(t, op.Params[0].Examples, "the unconvertible example is skipped, not appended")
	require.Equal(t, 1, openapitest.CountDiagsAt(diags, diag.DegradedConstruct, ir.SeverityWarning))
	d, ok := openapitest.FirstDegradedWarning(diags)
	require.True(t, ok)
	assert.Equal(t, "/paths/~1items/get/parameters/0/example", d.Provenance.Pointer)
	assert.Contains(t, d.Message, "example:")
}

func TestParams_SchemaConstraints(t *testing.T) {
	t.Parallel()
	spec := openapitest.PathsSpec(`  /people:
    get:
      operationId: listPeople
      parameters:
        - {name: age, in: query, schema: {type: integer, maximum: 120}}
      responses: {"200": {description: ok}}
`)
	_, svc, diags := lowerServiceSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	op := openapitest.FirstOp(t, svc)
	require.Len(t, op.Params, 1)
	c := op.Params[0].Constraints
	require.NotNil(t, c, "param scalar constraints land via annotation.Constraints")
	require.NotNil(t, c.Max)
	assert.Equal(t, ir.BigVal("120"), *c.Max, "numeric bound read at full precision")
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
	op := openapitest.FindOp(t, doc, "search")
	byName := openapitest.IndexBy(op.Bindings.HTTP[0].ParamBindings, func(b ir.HTTPParamBinding) string { return b.Param })
	assert.Equal(t, ir.HTTPLocationPath, byName["id"].Location)
	assert.Equal(t, ir.HTTPLocationQuery, byName["q"].Location)
	assert.Equal(t, ir.HTTPLocationHeader, byName["X-Tok"].Location)
	assert.Equal(t, ir.HTTPLocationCookie, byName["sid"].Location)
	assert.Equal(t, "deepObject", byName["filter"].Style)
	assert.Equal(t, "application/json", byName["complex"].ContentType)

	logical := openapitest.IndexBy(op.Params, func(p ir.Parameter) string { return p.Name.Source })
	assert.True(t, logical["id"].Required, "path param always required")
	require.NotNil(t, logical["q"].Deprecation)
	assert.NotEmpty(t, logical["q"].Examples)
	assert.NotEmpty(t, logical["filter"].Unmodeled)
	require.NotNil(t, logical["q"].Constraints)

	assert.True(t, openapitest.CountDiagsAt(diags, diag.DegradedConstruct, ir.SeverityWarning) > 0, "malformed param default warns")
	assert.True(t, openapitest.HasDiag(diags, diag.NumericPrecision), "malformed param constraint warns")
}

// TestParams_QueryStringLocation pins the whole of a querystring binding's
// declared serialization: the 3.2 location, and the media type from content
// carrying it alone. Style and explode are not legal keywords there, so the
// binding takes neither rather than the query defaults (GitHub #334).
func TestParams_QueryStringLocation(t *testing.T) {
	t.Parallel()
	spec := openapitest.PathsSpecVer("3.2.0", `  /q:
    get:
      operationId: q
      parameters:
        - name: qs
          in: querystring
          content:
            application/x-www-form-urlencoded:
              schema: {type: object, properties: {page: {type: string}}}
      responses:
        "200": {description: ok}
`)
	doc, diags := parseFull(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	op := openapitest.FindOp(t, doc, "q")
	require.Len(t, op.Bindings.HTTP, 1)
	require.Len(t, op.Bindings.HTTP[0].ParamBindings, 1)

	qs := op.Bindings.HTTP[0].ParamBindings[0]
	assert.Equal(t, ir.HTTPLocationQuerystring, qs.Location)
	assert.Equal(t, "application/x-www-form-urlencoded", qs.ContentType)
	assert.Empty(t, qs.Style, "3.2 binds the query string from content and forbids style there")
	assert.Nil(t, qs.Explode, "and explode with it: it qualifies a style, and there is none")
}

// TestParams_QueryStringDeclaredStyleIsKeptAndReported pins the other half of
// the same rule: the compiler stopped inventing a style at that location, it did
// not start erasing one. A document declaring style there is invalid and the
// parser says so; what it declared still lowers, because dropping declared
// content is an emitter's call rather than a compiler's.
func TestParams_QueryStringDeclaredStyleIsKeptAndReported(t *testing.T) {
	t.Parallel()
	spec := openapitest.PathsSpecVer("3.2.0", `  /q:
    get:
      operationId: q
      parameters:
        - name: qs
          in: querystring
          style: form
          explode: false
          content:
            application/x-www-form-urlencoded:
              schema: {type: object}
      responses:
        "200": {description: ok}
`)
	doc, diags := parseFull(t, spec)
	openapitest.AssertHasCode(t, diags, diag.Validation+"/"+string(validation.RuleValidationAllowedValues), ir.SeverityError)
	op := openapitest.FindOp(t, doc, "q")
	require.Len(t, op.Bindings.HTTP, 1)
	require.Len(t, op.Bindings.HTTP[0].ParamBindings, 1)

	qs := op.Bindings.HTTP[0].ParamBindings[0]
	require.Equal(t, ir.HTTPLocationQuerystring, qs.Location,
		"the keywords below are only news at the location that forbids them")
	assert.Equal(t, "form", qs.Style, "the declared style lowers as declared")
	require.NotNil(t, qs.Explode)
	assert.False(t, *qs.Explode, "and so does the explode qualifying it")
}

// TestParams_QueryStringDeclaredExplodeAloneIsKept covers the half of that rule
// the case above cannot see, because it declares both keywords: an explode
// written without a style beside it.
//
// Suppressing the invented style must not take a declared explode with it. 3.2
// forbids explode at this location as it forbids style, and the bundled parser
// refuses neither — so this reaches the compiler, and erasing it would be the
// same silent drop the invented style was, in the other direction.
func TestParams_QueryStringDeclaredExplodeAloneIsKept(t *testing.T) {
	t.Parallel()
	spec := openapitest.PathsSpecVer("3.2.0", `  /q:
    get:
      operationId: q
      parameters:
        - name: qs
          in: querystring
          explode: false
          content:
            application/x-www-form-urlencoded:
              schema: {type: object}
      responses:
        "200": {description: ok}
`)
	doc, diags := parseFull(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	op := openapitest.FindOp(t, doc, "q")
	require.Len(t, op.Bindings.HTTP, 1)
	require.Len(t, op.Bindings.HTTP[0].ParamBindings, 1)

	qs := op.Bindings.HTTP[0].ParamBindings[0]
	require.Equal(t, ir.HTTPLocationQuerystring, qs.Location,
		"the keywords below are only news at the location that forbids them")
	assert.Empty(t, qs.Style, "no style is invented at this location")
	require.NotNil(t, qs.Explode, "but the declared explode is not dropped with it")
	assert.False(t, *qs.Explode)
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
	openapitest.RequireNoErrorDiags(t, diags)
	getA := openapitest.FindOp(t, doc, "getA")
	getB := openapitest.FindOp(t, doc, "getB")
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
	openapitest.RequireNoErrorDiags(t, diags)
	getA := openapitest.FindOp(t, doc, "getA")
	getB := openapitest.FindOp(t, doc, "getB")
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
	_, svc, diags := lowerServiceSpec(t, openapitest.PathsSpec(
		"  /x:\n    get:\n      operationId: g\n      parameters:\n"+
			"        - name: q\n          in: query\n          schema: "+openapitest.InlineProbeBody+"\n"+
			"      responses: {\"204\": {description: ok}}\n"))
	openapitest.RequireNoErrorDiags(t, diags)

	p := openapitest.FirstOp(t, svc).Params[0]
	openapitest.AssertProbeDocsKept(t, p.Docs)
	assert.NotNil(t, p.Deprecation)
	openapitest.AssertProbeExample(t, p.Examples)
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
			"        - name: q\n          in: query\n          schema: "+openapitest.InlineProbeBody+"\n"+
			"      responses: {\"204\": {description: ok}}\n"+
			"components:\n  schemas:\n"+
			"    Outsider: {$ref: '#/paths/~1x/get/parameters/0/schema'}\n")
	openapitest.RequireNoErrorDiags(t, diags)

	p := openapitest.FirstOp(t, svc).Params[0]
	assert.Equal(t, ir.TypeID("t/prim/string"), p.Type.Target,
		"the parameter's own type is unchanged by the outside reference")
	openapitest.AssertProbeDocsKept(t, p.Docs)
	assert.NotNil(t, p.Deprecation)
	assert.Contains(t, p.Unmodeled, "openapi:x-vendor")
	assert.Contains(t, p.Unmodeled, "openapi:not")

	sc, ok := doc.Types["t/anon/paths/~1x/get/parameters/0/schema"].(*ir.Scalar)
	require.True(t, ok, "and the referenced pointer still names the schema written there")
	openapitest.AssertProbeDocsKept(t, sc.Docs)
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
	return openapitest.IndexBy(openapitest.FirstOp(t, svc).Params, func(p ir.Parameter) string { return p.Name.Source })
}

// TestParams_RefSchemaInheritsFromItsReferent covers the referent-fallback half
// of the $ref rule at a parameter (GitHub #131). A parameter whose schema is a
// bare $ref used to carry nothing at all — no description, no deprecation, no
// default — because the position never resolved the target it was holding.
func TestParams_RefSchemaInheritsFromItsReferent(t *testing.T) {
	t.Parallel()
	_, svc, diags := lowerServiceSpec(t, paramRefInheritSpec)
	openapitest.RequireNoErrorDiags(t, diags)

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
	openapitest.RequireNoErrorDiags(t, diags)

	p := paramsOf(t, svc)["override"]
	assert.Equal(t, "USE_SITE", p.Docs.Description, "the use-site description wins")
	require.NotNil(t, p.Default)
	assert.Equal(t, "use-site", p.Default.Str, "and the use-site default")
	assert.Nil(t, p.Deprecation, "and an explicit deprecated: false suppresses the referent's true")
}

// TestParams_RefSchemaInheritsThroughARefChain pins which resolution the
// fallback must use. Hop writes nothing but its own $ref, so a one-hop referent
// (annotation.At's) would leave the parameter with nothing; only following the
// chain to its end reaches Q.
func TestParams_RefSchemaInheritsThroughARefChain(t *testing.T) {
	t.Parallel()
	_, svc, diags := lowerServiceSpec(t, paramRefInheritSpec)
	openapitest.RequireNoErrorDiags(t, diags)

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
	openapitest.RequireNoErrorDiags(t, diags)
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
				openapitest.DiagMessageAt(t, diags, diag.DegradedConstruct, ir.SeverityInfo, tc.schemaPtr+"/xml"),
				"xml hints", "and announced once, at the same keyword the entry locates")
		})
	}
}

// TestParams_SchemaXMLHintsStayOnAnOwnedNode is the other half of one home per
// declaration: a parameter schema that hoisted a node of its own already has a
// structural XML field, so preserving a second copy on the parameter would give
// one declaration two homes that can drift.
func TestParams_SchemaXMLHintsStayOnAnOwnedNode(t *testing.T) {
	t.Parallel()
	doc, svc, diags := lowerServiceSpec(t, openapitest.PathsSpec(
		"  /x:\n    get:\n      operationId: g\n      parameters:\n"+
			"        - {name: obj, in: query, schema: {type: object, xml: {name: XOBJ}}}\n"+
			"      responses: {\"204\": {description: ok}}\n"))
	openapitest.RequireNoErrorDiags(t, diags)

	p := paramsOf(t, svc)["obj"]
	node, ok := doc.Types[p.Type.Target]
	require.True(t, ok, "the object schema owns a node of its own")
	require.NotNil(t, node.Common().XML)
	assert.Equal(t, "XOBJ", node.Common().XML.Name)
	assert.NotContains(t, p.Unmodeled, "openapi:xml", "so the parameter keeps no second copy")
	assert.Equal(t, 0, openapitest.CountDiagsAt(diags, diag.DegradedConstruct, ir.SeverityInfo),
		"and nothing is announced as homeless")
}

const paramVisibilitySpec = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /x:
    get:
      operationId: g
      parameters:
        - {name: ro, in: query, schema: {type: string, default: dq, readOnly: true}}
        - {name: wo, in: query, schema: {type: string, writeOnly: true}}
        - name: c
          in: query
          content:
            application/xml:
              schema: {type: string, readOnly: true}
      responses: {"200": {description: ok}}
`

// TestParams_SchemaVisibilityKeptUnderUnmodeled covers the residue keywords a
// parameter has no field for. ir-design §14 lowers readOnly/writeOnly to a
// Visibility, which only ir.Property carries, so at a parameter both reached no
// field and were dropped with no diagnostic at all — the silent loss this
// compiler exists to make impossible.
func TestParams_SchemaVisibilityKeptUnderUnmodeled(t *testing.T) {
	t.Parallel()
	_, svc, diags := lowerServiceSpec(t, paramVisibilitySpec)
	openapitest.RequireNoErrorDiags(t, diags)
	params := paramsOf(t, svc)

	cases := []struct{ param, keyword, schemaPtr string }{
		{"ro", "readOnly", "/paths/~1x/get/parameters/0/schema"},
		{"wo", "writeOnly", "/paths/~1x/get/parameters/1/schema"},
		{"c", "readOnly", "/paths/~1x/get/parameters/2/content/application~1xml/schema"},
	}
	for _, tc := range cases {
		t.Run(tc.param, func(t *testing.T) {
			t.Parallel()
			at := tc.schemaPtr + "/" + tc.keyword
			entry, ok := params[tc.param].Unmodeled["openapi:"+tc.keyword]
			require.True(t, ok, "%s has no ir.Parameter field, so it is kept raw, not dropped", tc.keyword)
			assert.Equal(t, ir.ReasonNoIRHome, entry.Reason)
			assert.JSONEq(t, `true`, string(entry.Value), "kept verbatim")
			assert.Equal(t, at, entry.Provenance.Pointer, "located at the keyword itself")
			assert.Contains(t,
				openapitest.DiagMessageAt(t, diags, diag.DegradedConstruct, ir.SeverityInfo, at),
				tc.keyword+" has no ir.Parameter home", "and announced once")
		})
	}
}

// TestParams_SchemaDefaultIsNotAlsoKeptVerbatim is the other side of that list:
// `default` is a residue keyword too, but Parameter.Default is a real home for
// it, so a verbatim copy beside it would give one declaration two homes.
func TestParams_SchemaDefaultIsNotAlsoKeptVerbatim(t *testing.T) {
	t.Parallel()
	_, svc, diags := lowerServiceSpec(t, paramVisibilitySpec)
	openapitest.RequireNoErrorDiags(t, diags)

	p := paramsOf(t, svc)["ro"]
	require.NotNil(t, p.Default, "the default lands in its own field")
	assert.Equal(t, "dq", p.Default.Str)
	assert.NotContains(t, p.Unmodeled, "openapi:default", "and is not restated verbatim beside it")
	for _, d := range diags {
		assert.NotEqual(t, "/paths/~1x/get/parameters/0/schema/default", d.Provenance.Pointer,
			"nor announced as homeless; got %+v", d)
	}
}

// TestParams_OwnAnnotationsWinOverTheSchema checks the precedence a parameter
// shares with a header: the parameter object describes this input, its schema
// describes the type, and the more specific of the two wins.
func TestParams_OwnAnnotationsWinOverTheSchema(t *testing.T) {
	t.Parallel()
	_, svc, diags := lowerServiceSpec(t, openapitest.PathsSpec(
		"  /x:\n    get:\n      operationId: g\n      parameters:\n"+
			"        - name: q\n          in: query\n          description: PARAM\n"+
			"          x-scope: param\n          schema: {type: string, description: SCHEMA, x-scope: schema}\n"+
			"      responses: {\"204\": {description: ok}}\n"))
	openapitest.RequireNoErrorDiags(t, diags)

	p := openapitest.FirstOp(t, svc).Params[0]
	assert.Equal(t, "PARAM", p.Docs.Description, "the parameter's own description wins")
	raw, ok := p.Unmodeled["openapi:x-scope"]
	require.True(t, ok)
	assert.JSONEq(t, `"param"`, string(raw.Value), "and its own extension overlays the schema's")
}

// TestParams_SchemaVisibilityKeptWhenTheSchemaOwnsANode pins the half of the
// parameter-visibility rescue that the own-node guard used to swallow. A
// parameter whose schema is an object, an enum or an array hoists a node, and
// neither home held the keywords: recordDeclarationResidue skips the position
// because a parameter is an annotation.HomeCarrier, and ir.Parameter has no
// Visibility field, so they were dropped for exactly the shapes that own a
// node.
func TestParams_SchemaVisibilityKeptWhenTheSchemaOwnsANode(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.PathsSpec(`  /x:
    get:
      parameters:
        - {name: obj, in: query, schema: {type: object, properties: {a: {type: string}}, readOnly: true}}
        - {name: arr, in: query, schema: {type: array, items: {type: string}, writeOnly: true}}
        - {name: enm, in: query, schema: {type: string, enum: [a, b], readOnly: true}}
      responses: {"200": {description: ok}}
`))
	op := openapitest.FindOp(t, doc, "")
	require.Len(t, op.Params, 3)

	for i, want := range []string{"openapi:readOnly", "openapi:writeOnly", "openapi:readOnly"} {
		param := op.Params[i]
		entry, ok := param.Unmodeled[want]
		require.True(t, ok, "%s: %s kept on the carrier that has no field for it", param.Name.Source, want)
		assert.Equal(t, ir.ReasonNoIRHome, entry.Reason)
		openapitest.AssertInfoDiagAt(t, diags, entry.Provenance.Pointer)
	}
}

// TestParams_AllowEmptyValueKept pins the last unread Parameter field. Style,
// explode, allowReserved and a content-style media type all reach
// ir.HTTPParamBinding; allowEmptyValue reached nothing, so a document declaring
// it compiled to IR identical to one that did not, and said nothing about it.
//
// The false spelling is asserted beside the true one because presence, not
// truth, is what is kept: a compiler recording only `true` would be deciding
// which declarations count.
func TestParams_AllowEmptyValueKept(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.PathsSpec(`  /x:
    get:
      operationId: getX
      parameters:
        - {name: on, in: query, allowEmptyValue: true, schema: {type: string}}
        - {name: off, in: query, allowEmptyValue: false, schema: {type: string}}
        - {name: silent, in: query, schema: {type: string}}
      responses: {"200": {description: ok}}
`))
	openapitest.RequireNoErrorDiags(t, diags)
	params := openapitest.IndexBy(openapitest.FindOp(t, doc, "getX").Params, func(p ir.Parameter) string { return p.Name.Source })

	for _, tc := range []struct{ name, want, index string }{
		{"on", `true`, "0"},
		{"off", `false`, "1"},
	} {
		entry, ok := params[tc.name].Unmodeled["openapi:allowEmptyValue"]
		require.True(t, ok, "%s keeps its declared flag", tc.name)
		assert.Equal(t, ir.ReasonNoIRHome, entry.Reason)
		assert.JSONEq(t, tc.want, string(entry.Value))
		assert.Equal(t, "/paths/~1x/get/parameters/"+tc.index+"/allowEmptyValue",
			entry.Provenance.Pointer, "kept at the keyword's own coordinate")
		openapitest.AssertInfoDiagAt(t, diags, entry.Provenance.Pointer)
	}

	assert.NotContains(t, params["silent"].Unmodeled, "openapi:allowEmptyValue",
		"a parameter that declares nothing records nothing")
}

// TestParams_ReservedHeaderNamesAreReported pins a deviation the compiler takes
// knowingly. OpenAPI says a header parameter named Accept, Content-Type or
// Authorization SHALL be ignored; Morphic lowers it anyway, because dropping
// declared content is a loss and choosing between the two belongs to an emitter.
// What must not happen is doing that in silence, which is what it used to do: an
// emitter had no way to tell such a parameter from any other, and would generate
// one that fights the security scheme or the negotiated media type.
func TestParams_ReservedHeaderNamesAreReported(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		param    string
		in       string
		reported bool
	}{
		{"authorization", "Authorization", "header", true},
		{"accept", "Accept", "header", true},
		{"content type", "Content-Type", "header", true},
		{"lowercase spelling", "authorization", "header", true},
		{"ordinary header", "X-Trace", "header", false},
		{"same name in query", "Accept", "query", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, diags := parseFull(t, openapitest.PathsSpec(`  /x:
    get:
      operationId: getX
      parameters:
        - {name: `+tc.param+`, in: `+tc.in+`, schema: {type: string}}
      responses: {"200": {description: ok}}
`))
			openapitest.RequireNoErrorDiags(t, diags)
			op := openapitest.FindOp(t, doc, "getX")
			require.Len(t, op.Params, 1, "the parameter lowers either way; nothing is dropped")
			assert.Equal(t, tc.param, op.Params[0].Name.Source)

			assert.Equal(t, tc.reported,
				openapitest.HasDiagCodeAt(diags, diag.ReservedHeaderName, "/paths/~1x/get/parameters/0"),
				"reported at the parameter's own pointer")
			if tc.reported {
				openapitest.AssertHasCode(t, diags, diag.ReservedHeaderName, ir.SeverityWarning)
			}
		})
	}
}

// TestParams_RefSiteKeywordsAreKeptOnTheParameter covers the third
// annotation.HomeCarrier position of the $ref-sibling census (GitHub #283).
//
// A parameter whose schema is a $ref resolves straight to the target, so the
// keywords written beside that $ref have no node of their own and no field on
// ir.Parameter either — exactly the shape a property and a header are in, and
// they are kept the same way: verbatim on the carrier, announced at the schema
// position that wrote them.
func TestParams_RefSiteKeywordsAreKeptOnTheParameter(t *testing.T) {
	t.Parallel()
	spec := "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\n" +
		"paths:\n  /x:\n    get:\n      operationId: g\n      parameters:\n" +
		"        - {name: q, in: query, schema: {$ref: '#/components/schemas/Base', format: email}}\n" +
		"        - {name: r, in: query, schema: {$ref: '#/components/schemas/Base', minLength: 3}}\n" +
		"      responses: {\"200\": {description: ok}}\n" +
		"components:\n  schemas:\n    Base: {type: string}\n"
	_, svc, diags := lowerServiceSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)

	params := openapitest.IndexBy(openapitest.FirstOp(t, svc).Params, func(p ir.Parameter) string { return p.Name.Source })
	q, ok := params["q"]
	require.True(t, ok)
	entry, ok := q.Unmodeled["openapi:format"]
	require.True(t, ok, "an alias over the target has no Encoding field, so the format is kept")
	assert.Equal(t, ir.ReasonDegradedLowering, entry.Reason)
	assert.JSONEq(t, `"email"`, string(entry.Value))
	openapitest.AssertInfoDiagAt(t, diags, "/paths/~1x/get/parameters/0/schema")

	r, ok := params["r"]
	require.True(t, ok)
	assert.Empty(t, r.Unmodeled, "a bound at the same position always reached ir.Parameter.Constraints")
	require.NotNil(t, r.Constraints)
	assert.Equal(t, int64(3), *r.Constraints.MinLength)
}

// TestParams_CoDeclaredBoundKeptOnTheParameter covers the parameter carrier for
// a 2020-12 side that declares both of its bound keywords (GitHub #286).
// ir.Constraints holds one bound per side, so one keyword reaches no field of
// the constraints the parameter carries and is kept verbatim beside them —
// otherwise {minimum: 10, exclusiveMinimum: 0} lowers to what {minimum: 10}
// does, at the one carrier ir.Parameter owns rather than a node.
//
// Both directions are here for the reason the property cases are: a row where
// the exclusive keyword is the one kept passes on a reader that always kept that
// one.
func TestParams_CoDeclaredBoundKeptOnTheParameter(t *testing.T) {
	t.Parallel()
	_, svc, diags := lowerServiceSpec(t, openapitest.PathsSpec(
		"  /x:\n    get:\n      operationId: g\n      parameters:\n"+
			"        - {name: low, in: query, schema: {type: integer, minimum: 10, exclusiveMinimum: 0}}\n"+
			"        - {name: high, in: query, schema: {type: integer, maximum: 100, exclusiveMaximum: 5}}\n"+
			"        - {name: plain, in: query, schema: {type: integer, minimum: 10}}\n"+
			"      responses: {\"204\": {description: ok}}\n"))
	openapitest.RequireNoErrorDiags(t, diags)
	params := paramsOf(t, svc)

	cases := []struct {
		param, index, wantKept, wantRaw string
	}{
		{param: "low", index: "0", wantKept: "openapi:exclusiveMinimum", wantRaw: "0"},
		{param: "high", index: "1", wantKept: "openapi:maximum", wantRaw: "100"},
	}
	for _, tc := range cases {
		t.Run(tc.param, func(t *testing.T) {
			t.Parallel()
			at := "/paths/~1x/get/parameters/" + tc.index + "/schema/" +
				strings.TrimPrefix(tc.wantKept, "openapi:")
			require.NotNil(t, params[tc.param].Constraints, "the tighter bound still reaches a field")
			entry, ok := params[tc.param].Unmodeled[tc.wantKept]
			require.True(t, ok, "%s is kept beside the constraints it did not reach; got %v",
				tc.wantKept, params[tc.param].Unmodeled)
			assert.Equal(t, ir.ReasonDegradedLowering, entry.Reason)
			assert.JSONEq(t, tc.wantRaw, string(entry.Value))
			assert.Equal(t, at, entry.Provenance.Pointer, "located at the keyword itself")
		})
	}

	assert.Empty(t, params["plain"].Unmodeled,
		"a side writing one keyword has it in a field, so nothing is restated beside it")
}
