// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package convention_test

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// instantParam is a header parameter whose field an RFC defines as an
// HTTP-date, declared date-time, on an operation that also takes a path
// parameter.
const instantParam = `openapi: 3.0.3
info: {title: t, version: "1"}
paths:
  /files/{name}:
    put:
      operationId: putFile
      parameters:
        - {name: name, in: path, required: true, schema: {type: string}}
        - {name: If-Unmodified-Since, in: header, schema: {type: string, format: date-time}}
      responses: {"204": {description: ok}}
`

// compileUnder runs the whole compiler over src under opts and returns the
// document and its diagnostics, none of them an error.
func compileUnder(t *testing.T, src string, opts openapi.Options) (*ir.Document, []ir.Diagnostic) {
	t.Helper()
	doc, diags, err := openapi.New().Compile(t.Context(), []compilers.Source{openapitest.SourceOf(src)},
		compilers.Options{FormatOptions: opts})
	require.NoError(t, err)
	require.NotNil(t, doc)
	openapitest.RequireNoErrorDiags(t, diags)
	return doc, diags
}

// httpDateReports returns the pointers of the inferred-http-date reports
// among diags, each required to be info.
func httpDateReports(t *testing.T, diags []ir.Diagnostic) []string {
	t.Helper()
	var out []string
	for _, d := range diags {
		if d.Code == diag.InferredHTTPDate {
			assert.Equal(t, ir.SeverityInfo, d.Severity)
			out = append(out, string(d.Provenance.Pointer))
		}
	}
	return out
}

// TestHTTPDate_ReachesAHeaderParameter pins that a header parameter whose
// field an RFC defines as an HTTP-date takes the http-date encoding on the
// parameter itself, marked and reported once at the parameter, while its type
// is the instant it was and a path parameter beside it is read as it was.
func TestHTTPDate_ReachesAHeaderParameter(t *testing.T) {
	t.Parallel()
	doc, diags := compileUnder(t, instantParam, openapi.Options{})
	op := openapitest.FindOp(t, doc, "putFile")
	require.Len(t, op.Params, 2)

	assert.Nil(t, op.Params[0].Encoding)
	assert.Empty(t, op.Params[0].Provenance.Inferred)
	assert.Equal(t, &ir.Encoding{Name: "http-date"}, op.Params[1].Encoding)
	assert.Equal(t, ir.PrimTypeID(ir.PrimDatetimeOffset), op.Params[1].Type.Target)
	assert.Equal(t, openapi.HTTPDateInferenceHeuristic, op.Params[1].Provenance.Inferred)
	assert.Equal(t, []string{"/paths/~1files~1{name}/put/parameters/1"}, httpDateReports(t, diags))
	assert.Len(t, diags, 1, "nothing else is reported: %+v", diags)
}

// TestHTTPDate_ParameterRows pins which parameters take the encoding and which
// keep what they declare: one the policy has no field for, one of another
// name, one declared as text, a parameter in any other location whatever its
// name, and the policy off. A parameter whose schema names a component takes
// the encoding itself and leaves the component and the member that names it
// as declared, and one declared on a path item for two operations is encoded
// on both and reported once.
func TestHTTPDate_ParameterRows(t *testing.T) {
	t.Parallel()
	in := func(location, name, schema string) string {
		template := "/a"
		if location == "path" {
			template += "/{" + name + "}"
		}
		return `openapi: 3.0.3
info: {title: t, version: "1"}
paths:
  ` + template + `:
    get:
      operationId: getA
      parameters:
        - {name: ` + name + `, in: ` + location + `, required: ` + strconv.FormatBool(location == "path") +
			`, schema: ` + schema + `}
      responses: {"204": {description: ok}}
`
	}
	const instant = "{type: string, format: date-time}"
	for _, tc := range []struct {
		name string
		src  string
		opts openapi.Options
	}{
		{"a query parameter named date", in("query", "date", instant), openapi.Options{}},
		{"a query parameter named after a field", in("query", "Expires", instant), openapi.Options{}},
		{"a path parameter named after a field", in("path", "Date", instant), openapi.Options{}},
		{"a cookie named after a field", in("cookie", "Last-Modified", instant), openapi.Options{}},
		{"a header declared as text alone", in("header", "If-Unmodified-Since", "{type: string}"), openapi.Options{}},
		{"a header of another name", in("header", "X-Seen-At", instant), openapi.Options{}},
		{"the policy off", instantParam, openapi.Options{HTTPDateInference: openapi.HTTPDateInference{Disabled: true}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, diags := compileUnder(t, tc.src, tc.opts)
			for _, p := range allParams(doc) {
				assert.Nil(t, p.Encoding, p.Name.Source)
				assert.Empty(t, p.Provenance.Inferred, p.Name.Source)
			}
			assert.Empty(t, diags)
		})
	}
	t.Run("a parameter naming a component", func(t *testing.T) {
		t.Parallel()
		doc, diags := compileUnder(t, `openapi: 3.1.0
info: {title: t, version: "1"}
paths:
  /a:
    get:
      operationId: getA
      parameters:
        - {name: If-Modified-Since, in: header, schema: {$ref: '#/components/schemas/Stamp'}}
      responses: {"204": {description: ok}}
  /b:
    post:
      operationId: postB
      requestBody:
        content:
          application/json:
            schema: {type: object, properties: {at: {$ref: '#/components/schemas/Stamp'}}}
      responses: {"204": {description: ok}}
components:
  schemas:
    Stamp: {type: string, format: date-time}
`, openapi.Options{})
		get := openapitest.FindOp(t, doc, "getA")
		require.Len(t, get.Params, 1)
		assert.Equal(t, &ir.Encoding{Name: "http-date"}, get.Params[0].Encoding)
		assert.Equal(t, ir.TypeID("t/openapi/components/schemas/Stamp"), get.Params[0].Type.Target, "its type is still the component")
		stamp, ok := doc.Types[get.Params[0].Type.Target].(*ir.Scalar)
		require.True(t, ok)
		assert.Nil(t, stamp.Encoding, "the component is shared and says nothing of one use")

		body, ok := doc.Types[openapitest.BodyTarget(t, openapitest.FindOp(t, doc, "postB").Request)].(*ir.Model)
		require.True(t, ok)
		require.Len(t, body.Properties, 1)
		assert.Nil(t, body.Properties[0].Encoding, "the member that names it is read as declared")
		assert.Len(t, httpDateReports(t, diags), 1)
	})
	t.Run("a path item parameter of two operations", func(t *testing.T) {
		t.Parallel()
		doc, diags := compileUnder(t, `openapi: 3.1.0
info: {title: t, version: "1"}
paths:
  /a:
    parameters:
      - {name: If-Modified-Since, in: header, schema: {type: string, format: date-time}}
    get: {operationId: getA, responses: {"204": {description: ok}}}
    put: {operationId: putA, responses: {"204": {description: ok}}}
`, openapi.Options{})
		for _, name := range []string{"getA", "putA"} {
			op := openapitest.FindOp(t, doc, name)
			require.Len(t, op.Params, 1, name)
			assert.Equal(t, &ir.Encoding{Name: "http-date"}, op.Params[0].Encoding, name)
			assert.Equal(t, openapi.HTTPDateInferenceHeuristic, op.Params[0].Provenance.Inferred, name)
		}
		assert.Equal(t, []string{"/paths/~1a/parameters/0"}, httpDateReports(t, diags), "one declaration, one report")
	})
}

// allParams returns every parameter of every operation of doc.
func allParams(doc *ir.Document) []ir.Parameter {
	var out []ir.Parameter
	var walk func(gs []ir.OperationGroup)
	walk = func(gs []ir.OperationGroup) {
		for _, g := range gs {
			for _, op := range g.Operations {
				out = append(out, op.Params...)
			}
			walk(g.Groups)
		}
	}
	for _, svc := range doc.Services {
		walk(svc.Groups)
	}
	return out
}
