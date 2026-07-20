package openapi

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

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
	byName := map[string]ir.HTTPParamBinding{}
	for _, b := range op.Bindings.HTTP[0].ParamBindings {
		byName[b.Param] = b
	}
	assert.Equal(t, ir.HTTPLocationPath, byName["id"].Location)
	assert.Equal(t, ir.HTTPLocationQuery, byName["q"].Location)
	assert.Equal(t, ir.HTTPLocationHeader, byName["X-Tok"].Location)
	assert.Equal(t, ir.HTTPLocationCookie, byName["sid"].Location)
	assert.Equal(t, "deepObject", byName["filter"].Style)
	assert.Equal(t, "application/json", byName["complex"].ContentType)

	logical := map[string]ir.Parameter{}
	for _, p := range op.Params {
		logical[p.Name.Source] = p
	}
	assert.True(t, logical["id"].Required, "path param always required")
	require.NotNil(t, logical["q"].Deprecation)
	assert.NotEmpty(t, logical["q"].Examples)
	assert.NotEmpty(t, logical["filter"].Extensions)
	require.NotNil(t, logical["q"].Constraints)

	var sawBadDefault, sawBadNumeric bool
	for _, d := range diags {
		if d.Severity == ir.SeverityWarning && d.Code == codeDegradedConstruct {
			sawBadDefault = true
		}
		if d.Code == codeNumericPrecision {
			sawBadNumeric = true
		}
	}
	assert.True(t, sawBadDefault, "malformed param default warns")
	assert.True(t, sawBadNumeric, "malformed param constraint warns")
}
