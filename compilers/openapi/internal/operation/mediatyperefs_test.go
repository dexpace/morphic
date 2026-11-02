// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package operation_test

import (
	"maps"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/engine/reader"
	"github.com/dexpace/morphic/ir"
)

// mediaTypeUses is two operations reaching the Media Type Object ref names
// from a request body, a response, a parameter and a header, in the order
// first, second.
func mediaTypeUses(ref string, first, second bool) string {
	ops := map[bool]string{
		true: `  /a:
    post:
      operationId: postA
      requestBody:
        content:
          application/json: {$ref: '` + ref + `'}
      responses: {"204": {description: none}}
`,
		false: `  /b:
    get:
      operationId: getB
      parameters:
        - name: f
          in: query
          content:
            application/json: {$ref: '` + ref + `'}
      responses:
        "200":
          description: ok
          headers:
            X-Pet:
              content:
                application/json: {$ref: '` + ref + `'}
          content:
            application/json: {$ref: '` + ref + `'}
`,
	}
	return ops[first] + ops[second]
}

// compileMediaTypeUses compiles a source at root.yaml, beside other.yaml
// holding other, with the paths paths, and returns the document and every
// diagnostic. withReader passes the compile a reader of the directory, as the
// engine does; without one no other document is read.
func compileMediaTypeUses(t *testing.T, other, paths string, withReader bool) (*ir.Document, []ir.Diagnostic) {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "other.yaml"), []byte(other), 0o600))
	opts := compilers.Options{}
	if withReader {
		opts.Reader = reader.Disk{}
	}
	doc, diags, err := openapi.New().Compile(t.Context(), []compilers.Source{{
		Path: filepath.Join(dir, "root.yaml"),
		Data: []byte(`openapi: 3.2.0
info: {title: T, version: "1"}
paths:
` + paths + `  /inline:
    get:
      operationId: inline
      responses:
        "200":
          description: ok
          content:
            application/problem+json:
              schema: {type: object, properties: {title: {type: string}}}
components:
  mediaTypes:
    Pet:
      schema: {type: object, properties: {name: {type: string}}}
`),
	}}, opts)
	require.NoError(t, err)
	require.NotNil(t, doc)
	return doc, diags
}

// TestMediaTypeRef_OneDeclarationIsOneType pins that a Media Type Object every
// kind of content entry names by $ref lowers once, at the pointer declaring
// it, named for its key, in the document declaring it, whichever entry is
// lowered first (GitHub #856): a components/mediaTypes entry of the source and
// of another document, and an inline content entry of the source, with and
// without a reader, since the resolver finds the source's own media types
// where load holds them (docs/parser-library.md#media-type-references).
func TestMediaTypeRef_OneDeclarationIsOneType(t *testing.T) {
	t.Parallel()
	const other = `openapi: 3.2.0
info: {title: O, version: "1"}
paths: {}
components:
  schemas:
    Name: {type: string, maxLength: 9}
  mediaTypes:
    Pet:
      schema: {type: object, properties: {name: {$ref: '#/components/schemas/Name'}}}
`
	for name, tc := range map[string]struct {
		ref, id, hint string
		withReader    bool
	}{
		"the source's component": {"#/components/mediaTypes/Pet",
			"t/anon/components/mediaTypes/Pet/schema", "pet", false},
		"the source's component, read with a reader": {"#/components/mediaTypes/Pet",
			"t/anon/components/mediaTypes/Pet/schema", "pet", true},
		"another document's component": {"other.yaml#/components/mediaTypes/Pet",
			"t/anon@other.yaml/components/mediaTypes/Pet/schema", "pet", true},
		"an inline content entry": {"#/paths/~1inline/get/responses/200/content/application~1problem+json",
			"t/anon/paths/~1inline/get/responses/200/content/application~1problem+json/schema", "response", false},
		"an inline content entry, read with a reader": {
			"#/paths/~1inline/get/responses/200/content/application~1problem+json",
			"t/anon/paths/~1inline/get/responses/200/content/application~1problem+json/schema", "response", true},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			forward, diags := compileMediaTypeUses(t, other, mediaTypeUses(tc.ref, true, false), tc.withReader)
			openapitest.RequireNoErrorDiags(t, diags)
			backward, backDiags := compileMediaTypeUses(t, other, mediaTypeUses(tc.ref, false, true), tc.withReader)
			openapitest.RequireNoErrorDiags(t, backDiags)

			assert.Empty(t, cmp.Diff(forward.Types, backward.Types), "the types must not follow the declaration order")
			td, ok := forward.Types[ir.TypeID(tc.id)]
			require.True(t, ok, "types: %v", slices.Collect(maps.Keys(forward.Types)))
			assert.Equal(t, tc.hint, td.Common().Name.Hint)
			post, get := openapitest.FindOp(t, forward, "postA"), openapitest.FindOp(t, forward, "getB")
			for _, target := range []ir.TypeID{post.Request.Contents[0].Type.Target,
				get.Responses[0].Payload.Contents[0].Type.Target, get.Params[0].Type.Target,
				get.Responses[0].Headers[0].Type.Target} {
				assert.Equal(t, ir.TypeID(tc.id), target)
			}
			model, ok := td.(*ir.Model)
			require.True(t, ok)
			require.Len(t, model.Properties, 1)
			assert.NotEqual(t, ir.PrimTypeID(ir.PrimAny), model.Properties[0].Type.Target,
				"what the declaration holds, a schema $ref included, resolves where it is declared")
		})
	}
}

// TestMediaTypeRef_UseSiteDocsFoldAlongTheChain pins the summary and
// description written beside each $ref of a content entry's chain, an alias's
// included: a body's Content.Docs folds them over the declaration's
// description, the nearest winning, and a parameter's, whose carrier has no
// docs home, keeps what that fold takes verbatim, each from the position
// writing it (docs/compilers/openapi.md#reference-object-siblings).
func TestMediaTypeRef_UseSiteDocsFoldAlongTheChain(t *testing.T) {
	t.Parallel()
	_, svc, diags := compileService(t, openapitest.PathsSpecVer("3.2.0", `  /a:
    get:
      operationId: a
      parameters:
        - name: f
          in: query
          content:
            application/json: {$ref: '#/components/mediaTypes/Alias', summary: Use.}
      responses:
        "200":
          description: ok
          content:
            application/json: {$ref: '#/components/mediaTypes/Alias', summary: Use.}
components:
  mediaTypes:
    Alias: {$ref: '#/components/mediaTypes/Filter', description: From the alias.}
    Filter: {description: From the declaration., schema: {type: string}}
`))
	openapitest.RequireNoErrorDiags(t, diags)
	op := openapitest.FirstOp(t, svc)
	assert.Equal(t, ir.Docs{Summary: "Use.", Description: "From the alias."}, op.Responses[0].Payload.Contents[0].Docs)
	require.Len(t, op.Params, 1)
	kept := op.Params[0].Unmodeled
	assert.JSONEq(t, `"From the alias."`, string(kept["openapi:content/application~1json/description"].Value))
	assert.Equal(t, "/components/mediaTypes/Alias/description",
		string(kept["openapi:content/application~1json/description"].Provenance.Pointer))
	assert.JSONEq(t, `"Use."`, string(kept["openapi:content/application~1json/summary"].Value))
	assert.Equal(t, "/paths/~1a/get/parameters/0/content/application~1json/summary",
		string(kept["openapi:content/application~1json/summary"].Provenance.Pointer))
	assert.False(t, openapitest.HasDiag(diags, diag.UnknownObjectKey))
	assert.False(t, openapitest.HasDiag(diags, diag.UnknownKeyEntryTaken))
}

// TestMediaTypeRef_ReferencesInsideTheDeclarationResolve pins that what a
// declaration reached by $ref holds is resolved where it is declared: an
// example and an encoding header written as Reference Objects, and a schema
// $ref, each lowering with what it names rather than in no form.
func TestMediaTypeRef_ReferencesInsideTheDeclarationResolve(t *testing.T) {
	t.Parallel()
	doc, svc, diags := compileService(t, openapitest.PathsSpecVer("3.2.0", `  /a:
    post:
      operationId: a
      requestBody:
        content:
          multipart/form-data: {$ref: '#/components/mediaTypes/Upload'}
      responses: {"204": {description: none}}
components:
  schemas:
    Upload: {type: object, properties: {file: {type: string, format: binary}}}
  headers:
    Rate: {schema: {type: integer}}
  examples:
    One: {value: {file: x}}
  mediaTypes:
    Upload:
      schema: {$ref: '#/components/schemas/Upload'}
      examples:
        one: {$ref: '#/components/examples/One'}
      encoding:
        file:
          headers:
            X-Rate: {$ref: '#/components/headers/Rate'}
`))
	openapitest.RequireNoErrorDiags(t, diags)
	content := openapitest.FirstOp(t, svc).Request.Contents[0]
	assert.Equal(t, typeByName(doc, "Upload").Common().ID, content.Type.Target)
	require.Len(t, content.Examples, 1)
	require.NotNil(t, content.Examples[0].Value)
	require.Len(t, content.Encoding, 1)
	for _, part := range content.Encoding {
		require.Len(t, part.Headers, 1)
		assert.Equal(t, ir.PrimTypeID(ir.PrimInteger), part.Headers[0].Type.Target)
	}
	for _, kept := range []string{"examples/One", "headers/Rate", "mediaTypes/Upload"} {
		assert.NotContains(t, doc.Unmodeled, "openapi:components/"+kept, "a reached entry is not kept")
	}
}

// TestMediaTypeRef_AnUnresolvedRefLowersNothing pins a content entry whose $ref
// names nothing: load reports it once, at the $ref, and the entry lowers to no
// Content, while a parameter it types still names its media type.
func TestMediaTypeRef_AnUnresolvedRefLowersNothing(t *testing.T) {
	t.Parallel()
	_, svc, diags := compileService(t, openapitest.PathsSpecVer("3.2.0", `  /a:
    get:
      operationId: a
      parameters:
        - name: f
          in: query
          content:
            application/json: {$ref: '#/components/mediaTypes/Missing'}
      responses:
        "200":
          description: ok
          content:
            application/json: {$ref: '#/components/mediaTypes/Missing'}
            text/plain: {schema: {type: string}}
`))
	op := openapitest.FirstOp(t, svc)
	require.Len(t, op.Responses[0].Payload.Contents, 1)
	assert.Equal(t, "text/plain", op.Responses[0].Payload.Contents[0].MediaType)
	require.Len(t, op.Params, 1)
	assert.Equal(t, ir.PrimTypeID(ir.PrimAny), op.Params[0].Type.Target)
	for _, at := range []string{"/paths/~1a/get/parameters/0/content/application~1json",
		"/paths/~1a/get/responses/200/content/application~1json"} {
		assert.Equal(t, 1, countDiagsAt(diags, diag.UnresolvedRef, at), "at %s: %v", at, diags)
	}
}

// countDiagsAt counts the diagnostics of code at pointer.
func countDiagsAt(diags []ir.Diagnostic, code, pointer string) int {
	n := 0
	for _, d := range diags {
		if d.Code == code && string(d.Provenance.Pointer) == pointer {
			n++
		}
	}
	return n
}

// TestMediaTypeRef_DocsAnotherDocumentsAliasWritesAreReported pins the docs the
// fold takes from an alias in another document: a body's Content.Docs holds
// them, and a parameter's, which would keep them verbatim at a position no
// position of this compile places, reports that it does not.
func TestMediaTypeRef_DocsAnotherDocumentsAliasWritesAreReported(t *testing.T) {
	t.Parallel()
	const other = `openapi: 3.2.0
info: {title: O, version: "1"}
paths: {}
components:
  mediaTypes:
    Alias: {$ref: '#/components/mediaTypes/Filter', description: From the alias.}
    Filter: {schema: {type: string}}
`
	doc, diags := compileMediaTypeUses(t, other, mediaTypeUses("other.yaml#/components/mediaTypes/Alias", true, false),
		true)
	openapitest.RequireNoErrorDiags(t, diags)
	get := openapitest.FindOp(t, doc, "getB")
	assert.Equal(t, ir.Docs{Description: "From the alias."}, get.Responses[0].Payload.Contents[0].Docs)
	require.Len(t, get.Params, 1)
	assert.NotContains(t, get.Params[0].Unmodeled, "openapi:content/application~1json/description")
	assert.True(t, openapitest.HasDiagCodeAt(diags, diag.DegradedConstruct,
		"/paths/~1b/get/parameters/0/content/application~1json"), "got %v", diags)
}
