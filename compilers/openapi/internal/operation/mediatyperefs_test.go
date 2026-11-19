// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package operation_test

import (
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
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
// without a reader (docs/parser-library.md#media-type-references).
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
			"type/anonymous/components/mediaTypes/Pet/schema", "pet", false},
		"the source's component, read with a reader": {"#/components/mediaTypes/Pet",
			"type/anonymous/components/mediaTypes/Pet/schema", "pet", true},
		"another document's component": {"other.yaml#/components/mediaTypes/Pet",
			"type/anonymous@other.yaml/components/mediaTypes/Pet/schema", "pet", true},
		"an inline content entry": {"#/paths/~1inline/get/responses/200/content/application~1problem+json",
			"type/anonymous/paths/~1inline/get/responses/200/content/application~1problem+json/schema", "response", false},
		"an inline content entry, read with a reader": {
			"#/paths/~1inline/get/responses/200/content/application~1problem+json",
			"type/anonymous/paths/~1inline/get/responses/200/content/application~1problem+json/schema", "response", true},
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
			assert.NotEqual(t, ir.PrimitiveTypeID(ir.PrimitiveAny), model.Properties[0].Type.Target,
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
	assert.JSONEq(t, `"From the alias."`, string(kept["openapi:content/application~1json/reference/1/description"].Value))
	assert.Equal(t, "/components/mediaTypes/Alias/description",
		string(kept["openapi:content/application~1json/reference/1/description"].Provenance.Pointer))
	assert.JSONEq(t, `"Use."`, string(kept["openapi:content/application~1json/reference/0/summary"].Value))
	assert.Equal(t, "/paths/~1a/get/parameters/0/content/application~1json/summary",
		string(kept["openapi:content/application~1json/reference/0/summary"].Provenance.Pointer))
	assert.False(t, openapitest.HasDiag(diags, diag.UnknownObjectKey))
	assert.False(t, openapitest.HasDiag(diags, diag.UnknownKeyEntryAlreadyClaimed))
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
		assert.Equal(t, ir.PrimitiveTypeID(ir.PrimitiveInteger), part.Headers[0].Type.Target)
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
	assert.Equal(t, ir.PrimitiveTypeID(ir.PrimitiveAny), op.Params[0].Type.Target)
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

// TestMediaTypeRef_DocsAnotherDocumentsAliasWritesArePlacedThere pins the
// docs the fold takes from an alias in another document: a body's
// Content.Docs holds them, and a parameter keeps them verbatim where the alias
// writes them, in that document's Document.Sources entry, as the bindings'
// table places the alias.
func TestMediaTypeRef_DocsAnotherDocumentsAliasWritesArePlacedThere(t *testing.T) {
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
	kept, ok := get.Params[0].Unmodeled["openapi:content/application~1json/reference/1/description"]
	require.True(t, ok, "the alias's description is kept")
	assert.Equal(t, ir.Provenance{Source: 1, Pointer: "/components/mediaTypes/Alias/description"}, kept.Provenance,
		"where the alias writes it")
	assert.False(t, openapitest.HasDiagCodeAt(diags, diag.DegradedConstruct,
		"/paths/~1b/get/parameters/0/content/application~1json"), "nothing is left unplaced: %v", diags)
}

// TestMediaTypeRef_AnUnsetSiblingLeavesTheDeclarationsDocs pins a description
// written empty or null beside a content entry's $ref: it reads as unset, as
// the fold reads it, so the declaration's own description stands, in a body's
// Content.Docs and in what a parameter keeps alike.
func TestMediaTypeRef_AnUnsetSiblingLeavesTheDeclarationsDocs(t *testing.T) {
	t.Parallel()
	for _, written := range []string{"''", "null"} {
		t.Run(written, func(t *testing.T) {
			t.Parallel()
			_, svc, diags := compileService(t, openapitest.PathsSpecVer("3.2.0", `  /a:
    get:
      operationId: a
      parameters:
        - name: f
          in: query
          content:
            application/json: {$ref: '#/components/mediaTypes/M', description: `+written+`}
      responses:
        "200":
          description: ok
          content:
            application/json: {$ref: '#/components/mediaTypes/M', description: `+written+`}
components:
  mediaTypes:
    M: {schema: {type: string}, description: From the declaration.}
`))
			op := openapitest.FirstOp(t, svc)
			assert.Equal(t, ir.Docs{Description: "From the declaration."}, op.Responses[0].Payload.Contents[0].Docs)
			require.Len(t, op.Params, 1)
			kept := op.Params[0].Unmodeled["openapi:content/application~1json/description"]
			assert.JSONEq(t, `"From the declaration."`, string(kept.Value))
			assert.Equal(t, "/components/mediaTypes/M/description", string(kept.Provenance.Pointer))
			assert.False(t, openapitest.HasDiag(diags, diag.UnknownKeyEntryAlreadyClaimed))
		})
	}
}

// TestMediaTypeRef_ABodyFoldsAndAParameterKeepsEachPosition holds the two
// readers of a content entry's docs over every way the use site, the alias it
// names and the declaration write a key: left out, empty, null or text. A
// body's Content.Docs takes the nearest text, a declaration's summary, which a
// Media Type Object does not define, being no docs; a parameter keeps each
// position's text at that position's entry
// (docs/compilers/openapi.md#reference-object-siblings).
func TestMediaTypeRef_ABodyFoldsAndAParameterKeepsEachPosition(t *testing.T) {
	t.Parallel()
	spellings := []string{"absent", "empty", "null", "text"}
	for _, key := range []string{"summary", "description"} {
		for cell := range 64 {
			writes := [3]string{spellings[cell%4], spellings[cell/4%4], spellings[cell/16]}
			t.Run(key+"/"+strings.Join(writes[:], "-"), func(t *testing.T) {
				t.Parallel()
				assertFoldsAlike(t, key, writes)
			})
		}
	}
}

// assertFoldsAlike compiles a body and a parameter naming one chain whose use
// site, alias and declaration write key as writes spells it, and asserts the
// body takes the text of the nearest position writing one that documents the
// entry, and the parameter keeps each, from where it is written.
func assertFoldsAlike(t *testing.T, key string, writes [3]string) {
	t.Helper()
	positions := [3]string{"use", "alias", "declaration"}
	sibling := func(i int) string {
		return map[string]string{"absent": "", "empty": ", " + key + ": ''", "null": ", " + key + ": null",
			"text": ", " + key + ": From the " + positions[i] + "."}[writes[i]]
	}
	_, svc, diags := compileService(t, openapitest.PathsSpecVer("3.2.0", `  /a:
    get:
      operationId: a
      parameters:
        - name: f
          in: query
          content:
            application/json: {$ref: '#/components/mediaTypes/Alias'`+sibling(0)+`}
      responses:
        "200":
          description: ok
          content:
            application/json: {$ref: '#/components/mediaTypes/Alias'`+sibling(0)+`}
components:
  mediaTypes:
    Alias: {$ref: '#/components/mediaTypes/M'`+sibling(1)+`}
    M: {schema: {type: string}`+sibling(2)+`}
`))
	openapitest.RequireNoErrorDiags(t, diags)
	written := [3]string{
		"/paths/~1a/get/parameters/0/content/application~1json/",
		"/components/mediaTypes/Alias/",
		"/components/mediaTypes/M/",
	}
	want := ""
	for i := range written {
		if writes[i] == "text" && (key == "description" || positions[i] != "declaration") {
			want = "From the " + positions[i] + "."
			break
		}
	}
	op := openapitest.FirstOp(t, svc)
	docs := op.Responses[0].Payload.Contents[0].Docs
	assert.Equal(t, want, map[string]string{"summary": docs.Summary, "description": docs.Description}[key])

	// A parameter has no docs, so it folds nothing: each position's text is kept
	// at that position's own entry, the use's and the alias's as what the chain
	// writes beside its $ref, and the declaration's as the Media Type Object's.
	require.Len(t, op.Params, 1)
	entries := [3]string{"reference/0/", "reference/1/", ""}
	for i, at := range written {
		kept, ok := op.Params[0].Unmodeled["openapi:content/application~1json/"+entries[i]+key]
		if writes[i] != "text" {
			continue
		}
		require.True(t, ok, "%s's %s not kept", positions[i], key)
		assert.JSONEq(t, strconv.Quote("From the "+positions[i]+"."), string(kept.Value))
		assert.Equal(t, at+key, string(kept.Provenance.Pointer))
		reason := ir.ReasonNoIRHome
		if positions[i] == "declaration" && key == "summary" {
			reason = ir.ReasonOutOfScope // a key the Media Type Object does not define
		}
		assert.Equal(t, reason, kept.Reason, positions[i])
	}
}

// TestMediaTypeRef_ACarrierReadsTheDeclarationInItsDocument pins that a
// parameter or header whose content entry names a Media Type Object in another
// document reads its schema where that object is declared: what the parameter
// keeps verbatim is placed in that document, and a bound the schema's own node
// holds is not copied onto the header as well (GitHub #856).
func TestMediaTypeRef_ACarrierReadsTheDeclarationInItsDocument(t *testing.T) {
	t.Parallel()
	const other = `openapi: 3.2.0
info: {title: O, version: "1"}
paths: {}
components:
  mediaTypes:
    Q: {schema: {type: integer, minimum: 2, readOnly: true}}
    H: {schema: {type: array, items: {type: integer}, maxItems: 3}}
`
	doc, diags := compileMediaTypeUses(t, other, `  /b:
    get:
      operationId: getB
      parameters:
        - name: f
          in: query
          content:
            application/json: {$ref: 'other.yaml#/components/mediaTypes/Q'}
      responses:
        "200":
          description: ok
          headers:
            X-H:
              content:
                application/json: {$ref: 'other.yaml#/components/mediaTypes/H'}
`, true)
	openapitest.RequireNoErrorDiags(t, diags)
	get := openapitest.FindOp(t, doc, "getB")

	require.Len(t, get.Params, 1)
	kept, ok := get.Params[0].Unmodeled["openapi:content/application~1json/schema/readOnly"]
	require.True(t, ok, "kept: %v", get.Params[0].Unmodeled)
	require.Less(t, kept.Provenance.Source, len(doc.Sources))
	assert.Equal(t, "other.yaml", filepath.Base(doc.Sources[kept.Provenance.Source].Path), "placed where declared")
	assert.Equal(t, "/components/mediaTypes/Q/schema/readOnly", string(kept.Provenance.Pointer))

	require.Len(t, get.Responses[0].Headers, 1)
	header := get.Responses[0].Headers[0]
	list, ok := doc.Types[header.Type.Target].(*ir.List)
	require.True(t, ok, "the header's type: %s", header.Type.Target)
	require.NotNil(t, list.Constraints)
	assert.Equal(t, int64(3), *list.Constraints.MaxItems, "the list node holds the bound")
	assert.Nil(t, header.Constraints, "so the header holds no copy of it")
}

// TestMediaTypeRef_ABodyKeepsTheKeysBesideEachRef holds a request body's, a
// response's and the default's content entry, each a $ref through two aliases,
// to keeping on its Content what the use and each alias write beside the $ref,
// keyed by the position along the chain, beside the declaration's own key and
// apart from the summary the use folds into Docs; the rule table reports each.
func TestMediaTypeRef_ABodyKeepsTheKeysBesideEachRef(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.PathsSpecVer("3.2.0", `  /a:
    post:
      operationId: a
      requestBody:
        content:
          application/json: {$ref: '#/components/mediaTypes/A1', summary: s, x-a: request, x-use: 0}
      responses:
        "200":
          description: ok
          content:
            application/json: {$ref: '#/components/mediaTypes/A1', summary: s, x-a: ok, x-use: 0}
        default:
          description: error
          content:
            application/json: {$ref: '#/components/mediaTypes/A1', summary: s, x-a: default, x-use: 0}
components:
  mediaTypes:
    A1: {$ref: '#/components/mediaTypes/A2', x-a: a1, x-one: 1}
    A2: {$ref: '#/components/mediaTypes/M', x-a: a2}
    M: {schema: {type: string}, x-a: own}
`))
	a := opTree(t, doc, "a")
	for _, payload := range []struct{ name, at string }{
		{"request", "/paths/~1a/post/requestBody/content/application~1json"},
		{"ok", "/paths/~1a/post/responses/200/content/application~1json"},
		{"default", "/paths/~1a/post/responses/default/content/application~1json"},
	} {
		content := map[string]any{
			"request": valueAtPath(a, "request", "contents", "0"),
			"ok":      valueAtPath(a, "responses", "0", "payload", "contents", "0"),
			"default": valueAtPath(a, "default", "payload", "contents", "0"),
		}[payload.name]
		assert.Equal(t, "s", valueAtPath(content, "docs", "summary"), payload.name)
		assert.Empty(t, cmp.Diff(map[string]string{
			"openapi:x-a":               `vendor_extension "own" /components/mediaTypes/M/x-a`,
			"openapi:reference/0/x-a":   `out_of_scope "` + payload.name + `" ` + payload.at + `/x-a`,
			"openapi:reference/0/x-use": `out_of_scope 0 ` + payload.at + `/x-use`,
			"openapi:reference/1/x-a":   `out_of_scope "a1" /components/mediaTypes/A1/x-a`,
			"openapi:reference/1/x-one": `out_of_scope 1 /components/mediaTypes/A1/x-one`,
			"openapi:reference/2/x-a":   `out_of_scope "a2" /components/mediaTypes/A2/x-a`,
		}, entries(t, valueAtPath(content, "unmodeled"))), payload.name)
	}
	reported := make([]string, 0, len(diags))
	for _, d := range diags {
		require.Equal(t, diag.UnknownObjectKey, d.Code, d.Message)
		reported = append(reported, string(d.Provenance.Pointer))
	}
	assert.ElementsMatch(t, []string{
		"/paths/~1a/post/requestBody/content/application~1json/x-a",
		"/paths/~1a/post/requestBody/content/application~1json/x-use",
		"/paths/~1a/post/responses/200/content/application~1json/x-a",
		"/paths/~1a/post/responses/200/content/application~1json/x-use",
		"/paths/~1a/post/responses/default/content/application~1json/x-a",
		"/paths/~1a/post/responses/default/content/application~1json/x-use",
		"/components/mediaTypes/A1/x-a", "/components/mediaTypes/A1/x-one", "/components/mediaTypes/A2/x-a",
	}, reported)
}

// TestMediaTypeRef_ADocKeyNotKeptDrawsNoKeptInfo holds a summary beside a
// parameter content entry's $ref that converts to no JSON, and so is kept
// nowhere, to the finding saying so alone: the info saying the key is kept
// verbatim is drawn only for a key that is.
func TestMediaTypeRef_ADocKeyNotKeptDrawsNoKeptInfo(t *testing.T) {
	t.Parallel()
	_, diags := parseFull(t, openapitest.PathsSpecVer("3.2.0", `  /a:
    get:
      operationId: a
      parameters:
        - name: f
          in: query
          content:
            application/json: {$ref: '#/components/mediaTypes/M', summary: .nan}
      responses: {"204": {description: ok}}
components:
  mediaTypes:
    M: {schema: {type: string}}
`))
	const key = "/paths/~1a/get/parameters/0/content/application~1json/summary"
	assert.Equal(t, []string{
		"openapi/unpreservable-construct error " + key,
		"openapi/validation/validation-type-mismatch warning " + key,
	}, findings(diags))
}
