// This file is a package-level suite: it pins how the compile reads the
// reference objects another document writes in what a reference brings in
// from it (GitHub #751).
package openapi_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// TestExternalContent_AReferenceItWritesIsResolvedWhereItIsDeclared pins
// GitHub #751 on testdata/openapi/nested_refs, read by default. a.yaml's path
// item, mounted at /p and /q, writes parameter $refs into a.yaml, into b.yaml
// and back into the source, a response $ref into b.yaml whose header writes
// one into b.yaml, and a callback $ref naming its own callback by its file,
// which the library builds afresh at each resolution. Each resolves at both
// mounts and lowers where it is declared, and a schema $ref whose pointer
// passes a.yaml's own parameter reference reads on through it.
func TestExternalContent_AReferenceItWritesIsResolvedWhereItIsDeclared(t *testing.T) {
	t.Parallel()
	path := filepath.Join("..", "..", "testdata", "openapi", "nested_refs", "root.yaml")
	data, err := os.ReadFile(path)
	require.NoError(t, err)

	doc, diags := compileOnDisk(t, path, string(data), false)

	openapitest.RequireNoErrorDiags(t, diags)
	require.Len(t, doc.Sources, 3)
	assert.Equal(t, "a.yaml", filepath.Base(doc.Sources[1].Path))
	assert.Equal(t, "b.yaml", filepath.Base(doc.Sources[2].Path))
	for _, mount := range []string{"/p", "/q"} {
		op := operationAt(t, doc, "GET", mount)
		params := map[string]ir.Provenance{}
		for _, p := range op.Params {
			params[p.Name.Source] = p.Provenance
		}
		assert.Equal(t, map[string]ir.Provenance{
			"own":   {Source: 1, Pointer: "/components/parameters/Own"},
			"third": {Source: 2, Pointer: "/components/parameters/Third"},
			"back":  {Source: 0, Pointer: "/components/parameters/Back"},
		}, params, mount)
		require.Len(t, op.Responses, 1, mount)
		assert.Equal(t, "from b", op.Responses[0].Docs.Description, mount)
		require.Len(t, op.Responses[0].Headers, 1, mount)
		header := op.Responses[0].Headers[0]
		assert.Equal(t, ir.Provenance{Source: 2, Pointer: "/components/responses/R/headers/X-H"}, header.Provenance, mount)
		assert.Equal(t, ir.PrimTypeID(ir.PrimInteger), header.Type.Target, mount)
		require.Len(t, op.Bindings.HTTP, 1, mount)
		require.Len(t, op.Bindings.HTTP[0].Callbacks, 1, "%s: a.yaml's callback is read", mount)
	}

	through, ok := doc.Types["t/openapi/components/schemas/Through"].(*ir.Scalar)
	require.True(t, ok, "%T", doc.Types["t/openapi/components/schemas/Through"])
	assert.Equal(t, ir.TypeID("t/anon@a.yaml/components/parameters/Own/schema"), through.Base.Target,
		"the pointer reads on through a.yaml's parameter reference")
}

// TestExternalContent_AReferenceItWritesThatFailsIsReportedOnceWhereItIsWritten
// pins the report GitHub #751 found missing: a $ref another document writes
// that names nothing is reported once, as an unresolved-ref error in that
// document at the $ref, however many mounts reach the object holding it, and
// the position lowers without it. /o reads a.yaml first, so the library builds
// /x afresh for /p and for /q, and each object's $ref fails.
func TestExternalContent_AReferenceItWritesThatFailsIsReportedOnceWhereItIsWritten(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	layout(t, dir, map[string]string{"a.yaml": `openapi: 3.1.0
info: {title: A, version: "1"}
paths:
  /x:
    get:
      parameters:
        - $ref: '#/components/parameters/Missing'
        - {name: kept, in: query, schema: {type: string}}
      responses: {"200": {description: ok}}
  /y: {get: {responses: {"200": {description: ok}}}}
`})

	doc, diags := compileOnDisk(t, filepath.Join(dir, "root.yaml"), `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /o: {$ref: 'a.yaml#/paths/~1y'}
  /p: {$ref: 'a.yaml#/paths/~1x'}
  /q: {$ref: 'a.yaml#/paths/~1x'}
`, false)

	failed := unresolvedRefs(diags)
	require.Len(t, failed, 1, "%+v", diags)
	assert.Equal(t, ir.Provenance{Source: 1, Pointer: "/paths/~1x/get/parameters/0"}, failed[0].Provenance)
	assert.Equal(t, ir.SeverityError, failed[0].Severity)
	assert.True(t, strings.HasPrefix(failed[0].Message, `unresolved $ref "#/components/parameters/Missing": `),
		failed[0].Message)
	assert.Equal(t, 1, openapitest.CountDiagsAt(diags, diag.UnresolvedRef, ir.SeverityError))
	for _, mount := range []string{"/p", "/q"} {
		op := operationAt(t, doc, "GET", mount)
		require.Len(t, op.Params, 1, mount)
		assert.Equal(t, "kept", op.Params[0].Name.Source, mount)
	}
}

// TestExternalContent_WhatAReferenceItWritesReachesIsValidated pins that an
// object only another document's $ref reaches is validated as one the
// source's reaches (GitHub #751): b.yaml's parameter, whose in is no location,
// is reported at a.yaml's $ref, naming its position in b.yaml. Reached from
// the source too, it is reported once, at the source's $ref, whichever
// document is read first.
func TestExternalContent_WhatAReferenceItWritesReachesIsValidated(t *testing.T) {
	t.Parallel()
	const direct = `  /direct:
    get:
      parameters: [{$ref: 'b.yaml#/components/parameters/Bad'}]
      responses: {"200": {description: ok}}
`
	for name, tc := range map[string]struct {
		paths string
		at    ir.Provenance
	}{
		"reached only from a.yaml":    {"", ir.Provenance{Source: 1, Pointer: "/paths/~1x/get/parameters/0"}},
		"reached from the source too": {direct, ir.Provenance{Source: 0, Pointer: "/paths/~1direct/get/parameters/0"}},
		"reached from the source first": {
			direct + "  /z: {$ref: 'a.yaml#/paths/~1x'}\n",
			ir.Provenance{Source: 0, Pointer: "/paths/~1direct/get/parameters/0"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			layout(t, dir, map[string]string{
				"a.yaml": "openapi: 3.1.0\ninfo: {title: A, version: \"1\"}\npaths:\n  /x:\n    get:\n" +
					"      parameters: [{$ref: 'b.yaml#/components/parameters/Bad'}]\n" +
					"      responses: {\"200\": {description: ok}}\n",
				"b.yaml": "openapi: 3.1.0\ninfo: {title: B, version: \"1\"}\npaths: {}\ncomponents:\n  parameters:\n" +
					"    Bad: {name: bad, in: nowhere, schema: {type: string}}\n",
			})
			root := "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths:\n  /p: {$ref: 'a.yaml#/paths/~1x'}\n" +
				tc.paths

			_, diags := compileOnDisk(t, filepath.Join(dir, "root.yaml"), root, false)

			var found []ir.Diagnostic
			for _, d := range diags {
				if strings.HasPrefix(d.Code, diag.Validation+"/") {
					found = append(found, d)
				}
			}
			require.Len(t, found, 1, "%+v", diags)
			assert.Equal(t, tc.at, found[0].Provenance)
			assert.Equal(t, diag.Validation+"/validation-allowed-values", found[0].Code)
			assert.Contains(t, found[0].Message, `Parameter Object field "in" must be one of`)
			assert.True(t, strings.HasSuffix(found[0].Message, string(filepath.Separator)+"b.yaml"), found[0].Message)
		})
	}
}

// TestExternalContent_FieldsBesideAPathItemRefItWritesAreReported pins a path
// item $ref another document writes with fields beside it (GitHub #751): the
// $ref resolves, and the fields, which the load phase models only in the
// source, draw the warning that keeps them from being dropped in silence, at
// the $ref in that document (GitHub #823).
func TestExternalContent_FieldsBesideAPathItemRefItWritesAreReported(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	layout(t, dir, map[string]string{"a.yaml": `openapi: 3.1.0
info: {title: A, version: "1"}
paths:
  /x:
    get:
      responses: {"200": {description: ok}}
      callbacks:
        cb:
          '{$request.query.u}':
            $ref: '#/paths/~1y'
            post: {operationId: postBeside, responses: {"200": {description: ok}}}
  /y:
    get: {operationId: getY, responses: {"200": {description: ok}}}
`})

	doc, diags := compileOnDisk(t, filepath.Join(dir, "root.yaml"), `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /p: {$ref: 'a.yaml#/paths/~1x'}
`, false)

	openapitest.RequireNoErrorDiags(t, diags)
	var degraded []ir.Diagnostic
	for _, d := range diags {
		if d.Code == diag.DegradedConstruct {
			degraded = append(degraded, d)
		}
	}
	require.Len(t, degraded, 1, "%+v", diags)
	assert.Equal(t, ir.SeverityWarning, degraded[0].Severity)
	assert.Equal(t, ir.Provenance{Source: 1, Pointer: "/paths/~1x/get/callbacks/cb/{$request.query.u}"},
		degraded[0].Provenance)
	assert.Contains(t, degraded[0].Message, "not lowered")
	op := operationAt(t, doc, "GET", "/p")
	require.Len(t, op.Bindings.HTTP, 1)
	require.Len(t, op.Bindings.HTTP[0].Callbacks, 1)
	assert.Equal(t, []ir.OpID{"op/openapi/paths/~1p/get/callbacks/cb/{$request.query.u}/get"},
		op.Bindings.HTTP[0].Callbacks[0].Operations, "the $ref resolves, and the field beside it is not lowered")
}
