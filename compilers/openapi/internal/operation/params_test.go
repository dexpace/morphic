// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package operation_test

import (
	"encoding/json/jsontext"
	"testing"

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
	_, svc, diags := compileService(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	op := openapitest.FirstOp(t, svc)
	require.Len(t, op.Params, 5)
	require.Len(t, op.Bindings.HTTP, 1)
	bindings := openapitest.IndexBy(op.Bindings.HTTP[0].ParamBindings, func(b ir.HTTPParamBinding) string { return b.WireName })
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
	_, svc, diags := compileService(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	op := openapitest.FirstOp(t, svc)
	require.Len(t, op.Bindings.HTTP, 1)
	require.Len(t, op.Bindings.HTTP[0].ParamBindings, 1)
	binding := op.Bindings.HTTP[0].ParamBindings[0]
	assert.Equal(t, "filter", binding.WireName)
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
	_, svc, diags := compileService(t, spec)
	op := openapitest.FirstOp(t, svc)
	require.Len(t, op.Params, 1)
	assert.Empty(t, op.Params[0].Examples, "the unconvertible example is skipped, not appended")
	require.Equal(t, 1, openapitest.CountDiagsAt(diags, diag.DegradedConstruct, ir.SeverityWarning))
	d, ok := openapitest.FirstDegradedWarning(diags)
	require.True(t, ok)
	assert.Equal(t, jsontext.Pointer("/paths/~1items/get/parameters/0/example"), d.Provenance.Pointer)
	assert.Contains(t, d.Message, "example:")
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
	byName := openapitest.IndexBy(op.Bindings.HTTP[0].ParamBindings, func(b ir.HTTPParamBinding) string { return b.WireName })
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

	assert.Positive(t, openapitest.CountDiagsAt(diags, diag.DegradedConstruct, ir.SeverityWarning), "malformed param default warns")
	assert.Nil(t, logical["bad"].Default, "and is dropped with its warning, not kept as a zero value")
	assert.True(t, openapitest.HasDiag(diags, diag.NumericPrecision), "malformed param constraint warns")
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

const paramProvenanceSpec = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /pets/{petId}:
    parameters:
      - {name: petId, in: path, required: true, schema: {type: string}}
    get:
      operationId: getPet
      parameters:
        - {name: fields, in: query, schema: {type: string}}
        - {$ref: '#/components/parameters/Page'}
      responses: {"200": {description: ok}}
    delete:
      operationId: deletePet
      responses: {"200": {description: ok}}
components:
  parameters:
    Page: {name: page, in: query, schema: {type: integer}}
`

// TestParams_ProvenanceIsTheDeclaringPosition pins where a parameter says it
// came from (GitHub #423). The three positions a parameter can be written at
// each answer differently, and the merge is why: an operation's own entry sits
// under that operation, a $ref'd one under the component it names, and a
// path-item one under the path item — the last shared by every operation on the
// path. Only the inline entries tell an inherited parameter from a declared
// one: a $ref'd entry lands on its component from either mount, and the mount
// site is not recorded.
func TestParams_ProvenanceIsTheDeclaringPosition(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, paramProvenanceSpec)
	openapitest.RequireNoErrorDiags(t, diags)
	getPet := openapitest.FindOp(t, doc, "getPet")
	deletePet := openapitest.FindOp(t, doc, "deletePet")
	byName := openapitest.IndexBy(getPet.Params, func(p ir.Parameter) string { return p.Name.Source })
	require.Len(t, byName, 3, "two declared plus the inherited path-item one")

	assert.Equal(t, jsontext.Pointer("/paths/~1pets~1{petId}/get/parameters/0"), byName["fields"].Provenance.Pointer,
		"an operation's own entry is declared under that operation")
	assert.Equal(t, jsontext.Pointer("/components/parameters/Page"), byName["page"].Provenance.Pointer,
		"a $ref'd entry is declared at the component it names, not at the use site")

	const pathItem = "/paths/~1pets~1{petId}/parameters/0"
	assert.Equal(t, jsontext.Pointer(pathItem), byName["petId"].Provenance.Pointer,
		"an inherited entry keeps the path item's pointer rather than the operation it merged into")
	require.Len(t, deletePet.Params, 1)
	assert.Equal(t, jsontext.Pointer(pathItem), deletePet.Params[0].Provenance.Pointer,
		"and both operations on the path name the one declaration, not one pointer each")

	for name, p := range byName {
		assert.Equal(t, 0, p.Provenance.Source, "%s addresses the compiled source", name)
		assert.Empty(t, p.Provenance.Inferred, "%s is declared, not inferred", name)
	}
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

// TestParams_AllowEmptyValueLowers pins the last Parameter field to reach the
// binding. Style, explode, allowReserved and a content-style media type all
// reach ir.HTTPParamBinding; allowEmptyValue reached none of its fields, so a
// document declaring it lowered it only verbatim. Now it is the binding's
// AllowEmptyValue, false where the parameter declares false or nothing, which
// OpenAPI gives the same meaning, as it does allowReserved's.
func TestParams_AllowEmptyValueLowers(t *testing.T) {
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
	op := openapitest.FindOp(t, doc, "getX")
	require.Len(t, op.Bindings.HTTP, 1)
	byWire := openapitest.IndexBy(op.Bindings.HTTP[0].ParamBindings, func(b ir.HTTPParamBinding) string { return b.WireName })

	assert.True(t, byWire["on"].AllowEmptyValue)
	assert.False(t, byWire["off"].AllowEmptyValue)
	assert.False(t, byWire["silent"].AllowEmptyValue)
	for _, p := range op.Params {
		assert.NotContains(t, p.Unmodeled, "openapi:allowEmptyValue", "%s keeps nothing verbatim", p.Name.Source)
	}
	assert.False(t, openapitest.HasDiag(diags, diag.DegradedConstruct), "nothing is degraded: %+v", diags)
}

// TestParams_AllowEmptyValueUnderAStyleThatCannotWriteOneIsReported pins the
// rule OpenAPI states for a style whose empty value is n/a in its table:
// allowEmptyValue SHALL be ignored there. It still lowers as declared, and is
// reported where it is written, as an allowReserved with no effect is; a style
// that can write an empty value draws nothing.
func TestParams_AllowEmptyValueUnderAStyleThatCannotWriteOneIsReported(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.PathsSpec(`  /x:
    get:
      operationId: getX
      parameters:
        - {name: d, in: query, style: deepObject, allowEmptyValue: true, schema: {type: object}}
        - {name: s, in: query, style: spaceDelimited, allowEmptyValue: true, schema: {type: array, items: {type: string}}}
        - {name: p, in: query, style: pipeDelimited, allowEmptyValue: true, schema: {type: array, items: {type: string}}}
        - {name: f, in: query, style: form, allowEmptyValue: true, schema: {type: string}}
        - {name: u, in: query, allowEmptyValue: true, schema: {type: string}}
      responses: {"200": {description: ok}}
`))
	openapitest.RequireNoErrorDiags(t, diags)
	assert.Equal(t, []jsontext.Pointer{
		"/paths/~1x/get/parameters/0/allowEmptyValue",
		"/paths/~1x/get/parameters/1/allowEmptyValue",
		"/paths/~1x/get/parameters/2/allowEmptyValue",
	}, openapitest.PointersOf(diags, diag.InapplicableLocationKeyword))
	for _, b := range openapitest.FindOp(t, doc, "getX").Bindings.HTTP[0].ParamBindings {
		assert.True(t, b.AllowEmptyValue, "%s lowers as declared", b.WireName)
	}
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
