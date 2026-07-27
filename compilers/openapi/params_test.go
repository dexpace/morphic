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
	assert.NotEmpty(t, logical["filter"].Extensions)
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
