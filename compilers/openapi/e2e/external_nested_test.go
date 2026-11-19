// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

// This file is a package-level suite: it pins how the compile reads the
// reference objects another document writes in what a reference brings in
// from it (GitHub #751).
package e2e_test

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
	path := filepath.Join("..", "..", "..", "testdata", "openapi", "nested_refs", "root.yaml")
	data, err := os.ReadFile(path)
	require.NoError(t, err)

	doc, diags := compileOnDisk(t, path, string(data), false)

	openapitest.RequireNoErrorDiags(t, diags)
	require.Len(t, doc.Sources, 3)
	assert.Equal(t, "a.yaml", filepath.Base(doc.Sources[1].Path))
	assert.Equal(t, "b.yaml", filepath.Base(doc.Sources[2].Path))
	for _, mount := range []string{"/p", "/q"} {
		operation := operationAt(t, doc, "GET", mount)
		params := map[string]ir.Provenance{}
		for _, parameter := range operation.Params {
			params[parameter.Name.Source] = parameter.Provenance
		}
		assert.Equal(t, map[string]ir.Provenance{
			"own":   {Source: 1, Pointer: "/components/parameters/Own"},
			"third": {Source: 2, Pointer: "/components/parameters/Third"},
			"back":  {Source: 0, Pointer: "/components/parameters/Back"},
		}, params, mount)
		require.Len(t, operation.Responses, 1, mount)
		assert.Equal(t, "from b", operation.Responses[0].Docs.Description, mount)
		require.Len(t, operation.Responses[0].Headers, 1, mount)
		header := operation.Responses[0].Headers[0]
		assert.Equal(t, ir.Provenance{Source: 2, Pointer: "/components/responses/R/headers/X-H"}, header.Provenance, mount)
		assert.Equal(t, ir.PrimitiveTypeID(ir.PrimitiveInteger), header.Type.Target, mount)
		require.Len(t, operation.Bindings.HTTP, 1, mount)
		require.Len(t, operation.Bindings.HTTP[0].Callbacks, 1, "%s: a.yaml's callback is read", mount)
	}

	assert.Equal(t, ir.TypeID("type/anonymous@a.yaml/components/parameters/Own/schema"), useTarget(t, doc, "Through"),
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
		operation := operationAt(t, doc, "GET", mount)
		require.Len(t, operation.Params, 1, mount)
		assert.Equal(t, "kept", operation.Params[0].Name.Source, mount)
	}
}

// TestExternalContent_AFindingTwoTargetsShareIsReportedOnceAtItsNode pins a
// finding two checks make about one node: c.yaml's parameter, read as a header
// by the source and as a parameter by a.yaml, holds one bad schema, which each
// check finds. It is reported once, in c.yaml's entry at the keyword, whichever
// of the two the source declares first.
func TestExternalContent_AFindingTwoTargetsShareIsReportedOnceAtItsNode(t *testing.T) {
	t.Parallel()
	const viaA = "  /a: {$ref: 'a.yaml#/paths/~1a'}\n"
	const viaHeader = "  /z:\n    get:\n      responses:\n        \"200\":\n          description: ok\n" +
		"          headers: {H: {$ref: 'c.yaml#/components/parameters/N'}}\n"
	for name, paths := range map[string]string{"a.yaml first": viaA + viaHeader, "the header first": viaHeader + viaA} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			layout(t, dir, map[string]string{
				"a.yaml": "openapi: 3.1.0\ninfo: {title: A, version: \"1\"}\npaths:\n  /a:\n    get:\n" +
					"      parameters: [{$ref: 'c.yaml#/components/parameters/N'}]\n" +
					"      responses: {\"200\": {description: ok}}\n",
				"c.yaml": "openapi: 3.1.0\ninfo: {title: C, version: \"1\"}\npaths: {}\ncomponents:\n  parameters:\n" +
					"    N: {name: n, in: query, schema: {type: bogus}}\n",
			})
			root := "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths:\n" + paths

			doc, diags := compileOnDisk(t, filepath.Join(dir, "root.yaml"), root, false)

			var found []ir.Diagnostic
			for _, diagnostic := range diags {
				if strings.HasPrefix(diagnostic.Code, diag.Validation+"/") {
					found = append(found, diagnostic)
				}
			}
			require.Len(t, found, 1, "%+v", diags)
			assert.Equal(t, ir.Provenance{
				Source: 2, Pointer: "/components/parameters/N/schema/type", Position: ir.Position{Line: 6, Column: 44},
			}, found[0].Provenance)
			assert.Contains(t, found[0].Message, `Schema Object field "type" must be one of`)
			require.Len(t, doc.Sources, 3)
			assert.Equal(t, "c.yaml", filepath.Base(doc.Sources[2].Path))
		})
	}
}

// TestExternalContent_WhatAReferenceItWritesReachesIsValidated pins that an
// object only another document's $ref reaches is validated as one the
// source's reaches (GitHub #751): b.yaml's parameter, whose in is no location,
// is reported in b.yaml's entry at the keyword (GitHub #1030). Reached from the
// source too, it is reported once, there, whichever document is read first.
func TestExternalContent_WhatAReferenceItWritesReachesIsValidated(t *testing.T) {
	t.Parallel()
	const direct = `  /direct:
    get:
      parameters: [{$ref: 'b.yaml#/components/parameters/Bad'}]
      responses: {"200": {description: ok}}
`
	for name, paths := range map[string]string{
		"reached only from a.yaml":      "",
		"reached from the source too":   direct,
		"reached from the source first": direct + "  /z: {$ref: 'a.yaml#/paths/~1x'}\n",
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
				paths

			doc, diags := compileOnDisk(t, filepath.Join(dir, "root.yaml"), root, false)

			var found []ir.Diagnostic
			for _, diagnostic := range diags {
				if strings.HasPrefix(diagnostic.Code, diag.Validation+"/") {
					found = append(found, diagnostic)
				}
			}
			require.Len(t, found, 1, "%+v", diags)
			assert.Equal(t, ir.Provenance{
				Source: 2, Pointer: "/components/parameters/Bad/in", Position: ir.Position{Line: 6, Column: 26},
			}, found[0].Provenance)
			assert.Equal(t, diag.Validation+"/validation-allowed-values", found[0].Code)
			assert.Contains(t, found[0].Message, `Parameter Object field "in" must be one of`)
			require.Len(t, doc.Sources, 3)
			assert.Equal(t, "b.yaml", filepath.Base(doc.Sources[2].Path))
		})
	}
}

// TestExternalContent_FieldsBesideAPathItemRefItWritesAreLowered pins a path
// item $ref another document writes with fields beside it (GitHub #751): the
// $ref resolves, and the fields are modelled where that document writes them,
// so the operation beside it is lowered there, with no finding.
func TestExternalContent_FieldsBesideAPathItemRefItWritesAreLowered(t *testing.T) {
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
	for _, diagnostic := range diags {
		assert.NotEqual(t, diag.DegradedConstruct, diagnostic.Code, "%+v", diagnostic)
	}
	operation := operationAt(t, doc, "GET", "/p")
	require.Len(t, operation.Bindings.HTTP, 1)
	require.Len(t, operation.Bindings.HTTP[0].Callbacks, 1)
	const cb = "operation/openapi/paths/~1p/get/callbacks/cb/{$request.query.u}/"
	assert.Equal(t, []ir.OperationID{cb + "get", cb + "post"}, operation.Bindings.HTTP[0].Callbacks[0].Operations,
		"the $ref resolves, and the field beside it is lowered")
	post := operationWithID(t, doc, cb+"post")
	assert.Equal(t, ir.Provenance{Source: 1, Pointer: "/paths/~1x/get/callbacks/cb/{$request.query.u}/post"},
		post.Provenance, "declared where the other document writes it")
}

// TestConstraints_ANamedTypeIsReadInItsOwnDocument pins that the type a bound's
// schema names is the one its own document declares. The root declares an Arr
// of another type, so reading the name in the root's schemas would take the
// bound, which the array the other document names does not.
func TestConstraints_ANamedTypeIsReadInItsOwnDocument(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	layout(t, dir, map[string]string{"other.yaml": `openapi: 3.1.0
info: {title: o, version: "1"}
paths: {}
components:
  schemas:
    Use: {$ref: '#/components/schemas/Arr', minLength: 1}
    Box: {type: object, properties: {inner: {$ref: '#/components/schemas/Arr', minLength: 1}}}
    Arr: {type: array, items: {type: string}}
`})
	const root = `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    Arr: {type: string}
    Holder:
      type: object
      properties:
        far: {$ref: 'other.yaml#/components/schemas/Use'}
        box: {$ref: 'other.yaml#/components/schemas/Box'}
`
	doc, diags := compileOnDisk(t, filepath.Join(dir, "root.yaml"), root, true)
	openapitest.RequireNoErrorDiags(t, diags)
	require.Len(t, doc.Sources, 2)

	use, ok := doc.Types["type/openapi@other.yaml/components/schemas/Use"].(*ir.Scalar)
	require.True(t, ok, "%T", doc.Types["type/openapi@other.yaml/components/schemas/Use"])
	assert.Nil(t, use.Constraints)
	assert.Contains(t, use.Unmodeled, "openapi:minLength")

	box, ok := doc.Types["type/openapi@other.yaml/components/schemas/Box"].(*ir.Model)
	require.True(t, ok, "%T", doc.Types["type/openapi@other.yaml/components/schemas/Box"])
	inner, ok := propertyByWire(box, "inner")
	require.True(t, ok)
	assert.Nil(t, inner.Constraints)
	entry := unmodeledEntry(t, inner.Unmodeled, "openapi:minLength")
	assert.Equal(t, ir.Provenance{Source: 1, Pointer: "/components/schemas/Box/properties/inner/minLength"},
		ir.Provenance{Source: entry.Provenance.Source, Pointer: entry.Provenance.Pointer})

	var infos []ir.Diagnostic
	for _, diagnostic := range diags {
		if diagnostic.Code == diag.DegradedConstruct {
			infos = append(infos, diagnostic)
		}
	}
	require.Len(t, infos, 2, "%+v", diags)
	for _, diagnostic := range infos {
		assert.Equal(t, ir.SeverityInfo, diagnostic.Severity)
		assert.Equal(t, 1, diagnostic.Provenance.Source, "%s is reported in other.yaml", diagnostic.Provenance.Pointer)
	}
}
