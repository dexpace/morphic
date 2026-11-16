// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package service_test

import (
	"encoding/json/jsontext"
	"path/filepath"
	"testing"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/load"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/resolve"
	"github.com/dexpace/morphic/compilers/openapi/internal/service"
	"github.com/dexpace/morphic/ir"
)

// TestNew_KeepsTheDocumentItWasGiven pins the rest of the context: the document,
// grouping policy and its extension, source identity and index arrive
// unchanged, since every Provenance the compile stamps is built from the last
// two.
func TestNew_KeepsTheDocumentItWasGiven(t *testing.T) {
	t.Parallel()
	doc := openapitest.DocDeclaring("User")
	src := ir.SourceInfo{Format: "openapi@3.1", Path: "spec.yaml", Hash: "abc"}

	ext := service.GroupingExtension{Key: "x-group", Lists: service.ListsAreGroups, Fallback: service.GroupByPathPrefix}
	c := service.New(7, &load.Document{Doc: doc, Source: src},
		service.Policy{Grouping: service.GroupByExtension, GroupingExtension: ext})

	assert.Same(t, doc, c.Doc, "the document is referenced, never copied")
	assert.Equal(t, src, c.Source)
	assert.Equal(t, service.GroupByExtension, c.Grouping)
	assert.Equal(t, ext, c.GroupingExtension)
	assert.Equal(t, 7, c.SrcIndex)
}

// referenceKinds is a source declaring one component of each kind the library
// models as a reference, and beside each an alias naming it.
const referenceKinds = `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
components:
  pathItems: {P: {get: {responses: {"200": {description: ok}}}}, A: {$ref: '#/components/pathItems/P'}}
  parameters: {P: {name: q, in: query, schema: {type: string}}, A: {$ref: '#/components/parameters/P'}}
  headers: {P: {schema: {type: string}}, A: {$ref: '#/components/headers/P'}}
  requestBodies: {P: {content: {application/json: {schema: {type: string}}}}, A: {$ref: '#/components/requestBodies/P'}}
  responses: {P: {description: ok}, A: {$ref: '#/components/responses/P'}}
  examples: {P: {value: 1}, A: {$ref: '#/components/examples/P'}}
  links: {P: {operationRef: '#/paths/~1x/get'}, A: {$ref: '#/components/links/P'}}
  callbacks: {P: {'{$request.body#/u}': {post: {responses: {"200": {description: ok}}}}}, A: {$ref: '#/components/callbacks/P'}}
  securitySchemes: {P: {type: http, scheme: basic}, A: {$ref: '#/components/securitySchemes/P'}}
`

// TestDeclared_ReadsEachReferenceKind pins that a component alias of every
// kind the library models as a reference is read as one, so it is declared
// where its chain ends, as the bindings' table reads it. A kind the load phase
// left out of its index would lower at the alias.
func TestDeclared_ReadsEachReferenceKind(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "root.yaml")
	doc, _, err := load.Load(t.Context(), 0, compilers.Source{Path: path, Data: []byte(referenceKinds)}, load.Options{})
	require.NoError(t, err)
	c := service.New(0, doc, service.Policy{})
	cs := doc.Doc.Components
	for kind, decl := range map[string]jsontext.Pointer{
		"pathItems":       declaredAt[soa.PathItem](c, cs.PathItems.GetOrZero("A")),
		"parameters":      declaredAt[soa.Parameter](c, cs.Parameters.GetOrZero("A")),
		"headers":         declaredAt[soa.Header](c, cs.Headers.GetOrZero("A")),
		"requestBodies":   declaredAt[soa.RequestBody](c, cs.RequestBodies.GetOrZero("A")),
		"responses":       declaredAt[soa.Response](c, cs.Responses.GetOrZero("A")),
		"examples":        declaredAt[soa.Example](c, cs.Examples.GetOrZero("A")),
		"links":           declaredAt[soa.Link](c, cs.Links.GetOrZero("A")),
		"callbacks":       declaredAt[soa.Callback](c, cs.Callbacks.GetOrZero("A")),
		"securitySchemes": declaredAt[soa.SecurityScheme](c, cs.SecuritySchemes.GetOrZero("A")),
	} {
		assert.Equal(t, jsontext.Pointer("/components/"+kind+"/P"), decl, kind)
	}
}

// declaredAt is the pointer Declared declares what ref, written at /use,
// names at.
func declaredAt[T, S any, R interface {
	*S
	resolve.Referenced[T]
	GetCoreAny() any
}, P interface {
	*T
	GetRootNode() *yaml.Node
}](c service.Ctx, ref R) jsontext.Pointer {
	_, _, decl := service.Declared[T, S, R, P](c, ref, "/use")
	return decl
}

// TestSiblings_ReadsTheLoadPhasesModel holds where a lowering reads the fields
// beside a path item's $ref: from the load phase's model, which the context
// carries.
func TestSiblings_ReadsTheLoadPhasesModel(t *testing.T) {
	t.Parallel()
	spec := openapitest.PathsSpec(`  /a:
    $ref: '#/paths/~1b'
    put: {responses: {"200": {description: ok}}}
  /b:
    get: {responses: {"200": {description: ok}}}
`)
	doc, diags, err := load.Load(t.Context(), 0, compilers.Source{Path: "spec.yaml", Data: []byte(spec)}, load.Options{})
	require.NoError(t, err)
	openapitest.RequireNoErrorDiags(t, diags)
	c := service.New(0, doc, service.Policy{})
	rp, ok := doc.Doc.GetPaths().Get("/a")
	require.True(t, ok)

	sib, ok := c.Siblings().Of(rp)
	require.True(t, ok, "the context reads the load phase's model")
	assert.NotNil(t, sib.Item.Put(), "the operation beside the $ref is modelled")
	assert.Equal(t, jsontext.Pointer("/paths/~1a"), sib.Pointer)
}

// TestMediaTypeUse_ReadsTheLoadPhasesModel holds where a lowering reads the
// model of a content entry written as a $ref: from the load phase's, which the
// context carries.
func TestMediaTypeUse_ReadsTheLoadPhasesModel(t *testing.T) {
	t.Parallel()
	spec := openapitest.PathsSpecVer("3.2.0", `  /a:
    get:
      responses:
        "200":
          description: ok
          content:
            application/json: {$ref: '#/components/mediaTypes/M'}
            text/plain: {schema: {type: string}}
components:
  mediaTypes:
    M: {schema: {type: string}}
`)
	doc, diags, err := load.Load(t.Context(), 0, compilers.Source{Path: "spec.yaml", Data: []byte(spec)}, load.Options{})
	require.NoError(t, err)
	openapitest.RequireNoErrorDiags(t, diags)
	c := service.New(0, doc, service.Policy{})
	item, ok := doc.Doc.GetPaths().Get("/a")
	require.True(t, ok)
	response, ok := item.GetObject().Get().GetResponses().Get("200")
	require.True(t, ok)
	content := response.GetObject().GetContent()

	use, isUse := c.MediaTypeUse(content.GetOrZero("application/json"))
	require.True(t, isUse)
	require.NotNil(t, use)
	assert.True(t, use.IsReference(), "the context reads the load phase's model")
	_, isUse = c.MediaTypeUse(content.GetOrZero("text/plain"))
	assert.False(t, isUse, "an entry written inline is no use")
}
