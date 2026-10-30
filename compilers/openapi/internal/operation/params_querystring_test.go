// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package operation_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

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
// not start erasing one. A document declaring style there breaks 3.2's rule and
// is warned of, as explode is; what it declared still lowers, because dropping
// declared content is an emitter's call rather than a compiler's.
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
	openapitest.RequireNoErrorDiags(t, diags)
	for _, keyword := range []string{"style", "explode"} {
		assert.Equal(t, "parameter field "+keyword+" is not allowed for in=querystring; lowered as declared",
			openapitest.DiagMessageAt(t, diags, diag.InvalidLocationKeyword, ir.SeverityWarning,
				"/paths/~1q/get/parameters/0/"+keyword))
	}
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

// TestParams_QueryStringDeclaredExplodeAloneIsKeptAndReported covers the half
// of that rule the style case above cannot see, because it declares both
// keywords: an explode written without a style beside it.
//
// Suppressing the invented style must not take a declared explode with it. 3.2
// forbids explode at this location as it forbids style, and erasing it would be
// the same silent drop the invented style was, in the other direction. See
// diag.InvalidLocationKeyword's GoDoc for why the report is a warning.
func TestParams_QueryStringDeclaredExplodeAloneIsKeptAndReported(t *testing.T) {
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

	assert.Equal(t,
		"parameter field explode is not allowed for in=querystring; lowered as declared",
		openapitest.DiagMessageAt(t, diags, diag.InvalidLocationKeyword, ir.SeverityWarning,
			"/paths/~1q/get/parameters/0/explode"))
}

// TestParams_QueryStringDeclaredAllowReservedIsKeptAndReported is the third
// keyword 3.2 forbids at this location alongside style and explode; see
// diag.InvalidLocationKeyword's GoDoc for why the report is a warning.
func TestParams_QueryStringDeclaredAllowReservedIsKeptAndReported(t *testing.T) {
	t.Parallel()
	spec := openapitest.PathsSpecVer("3.2.0", `  /q:
    get:
      operationId: q
      parameters:
        - name: qs
          in: querystring
          allowReserved: true
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
		"the keyword below is only news at the location that forbids it")
	assert.True(t, qs.AllowReserved, "the declared allowReserved lowers as declared")

	assert.Equal(t,
		"parameter field allowReserved is not allowed for in=querystring; lowered as declared",
		openapitest.DiagMessageAt(t, diags, diag.InvalidLocationKeyword, ir.SeverityWarning,
			"/paths/~1q/get/parameters/0/allowReserved"))
}

// TestParams_QueryStringUndeclaredKeywordsAreNotReported is the control for the
// three cases above: a querystring binding that declares none of style, explode
// and allowReserved raises diag.InvalidLocationKeyword nowhere, so the
// diagnostic tracks each keyword's presence in the document rather than firing
// for every parameter at this location.
func TestParams_QueryStringUndeclaredKeywordsAreNotReported(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.PathsSpecVer("3.2.0", `  /q:
    get:
      operationId: q
      parameters:
        - name: qs
          in: querystring
          content:
            application/x-www-form-urlencoded:
              schema: {type: object}
      responses:
        "200": {description: ok}
`))
	openapitest.RequireNoErrorDiags(t, diags)
	op := openapitest.FindOp(t, doc, "q")
	require.Len(t, op.Bindings.HTTP, 1)
	require.Len(t, op.Bindings.HTTP[0].ParamBindings, 1)
	assert.False(t, openapitest.HasDiag(diags, diag.InvalidLocationKeyword))
}
