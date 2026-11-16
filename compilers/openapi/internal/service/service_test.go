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
	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/doctree"
	"github.com/dexpace/morphic/compilers/openapi/internal/load"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/overlay"
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

// TestNew_DerivesTheDeclaredSchemaNames pins the index the $ref and
// discriminator-mapping resolutions read. A name missing from it is not a
// resolvable target, so under-deriving turns a valid $ref into a dangling one;
// over-deriving mints an ID for a component the document never declared.
func TestNew_DerivesTheDeclaredSchemaNames(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		doc      *soa.OpenAPI
		declares []string
		denies   []string
	}{
		{
			name: "every declared component", doc: openapitest.DocDeclaring("User", "Order", "A~B"),
			declares: []string{"User", "Order", "A~B"}, denies: []string{"Missing", ""},
		},
		{
			name: "a document declaring none", doc: &soa.OpenAPI{},
			denies: []string{"User", ""},
		},
		{
			name: "components present but no schemas", doc: &soa.OpenAPI{Components: &soa.Components{}},
			denies: []string{"User"},
		},
		{
			name: "the empty name is a name like any other", doc: openapitest.DocDeclaring(""),
			declares: []string{""}, denies: []string{"User"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := service.New(0, &load.Document{Doc: tc.doc}, service.Policy{})
			for _, n := range tc.declares {
				assert.True(t, c.DeclaresSchema(n), "%q is declared", n)
			}
			for _, n := range tc.denies {
				assert.False(t, c.DeclaresSchema(n), "%q is not declared", n)
			}
		})
	}
}

// appliedOverlay returns a real Origin by applying ov to a tree of doc. Origin's
// fields are unexported, so a context carrying one can only be built the way the
// loader builds it — which is also what keeps this test honest about the shape
// the compiler actually hands over.
func appliedOverlay(t *testing.T, index int, doc, ov string) overlay.Origin {
	t.Helper()
	var root yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte(doc), &root))

	origin, diags := overlay.Apply(t.Context(), index, &root, overlayDoc(t, ov), overlay.Options{Path: "patch.yaml", Data: []byte(ov)})
	require.False(t, diag.HasError(diags), "%+v", diags)
	require.True(t, origin.Applied())
	return origin
}

// TestSources_ListsTheOverlayAfterTheSourceItPatched pins the list
// Provenance.Source indexes into. The order is the contract: a position
// attributed to index 1 must find the overlay there, so a Sources built in the
// other order would misname every one of them rather than fail.
func TestSources_ListsTheOverlayAfterTheSourceItPatched(t *testing.T) {
	t.Parallel()
	src := ir.SourceInfo{Format: "openapi@3.1", Path: "spec.yaml", Hash: "abc"}
	origin := appliedOverlay(t, 1, "info: {title: T}\n",
		"overlay: 1.0.0\ninfo: {title: O, version: \"1\"}\nactions:\n"+
			"  - target: $.info\n    update: {description: d}\n")

	c := service.New(0, &load.Document{Doc: openapitest.DocDeclaring(), Source: src, Overlay: origin}, service.Policy{})

	require.Len(t, c.Sources(), 2)
	assert.Equal(t, src, c.Sources()[0], "the source being lowered comes first")
	assert.Equal(t, "overlay@1.0.0", c.Sources()[1].Format, "and the overlay applied to it second")
	assert.Equal(t, "patch.yaml", c.Sources()[1].Path)
}

// TestSources_ListsOnlyTheSourceWhenNoOverlayApplied is the control for the case
// above: without it, an assertion that the overlay entry is present would pass
// on a context that appended one unconditionally.
func TestSources_ListsOnlyTheSourceWhenNoOverlayApplied(t *testing.T) {
	t.Parallel()
	src := ir.SourceInfo{Format: "openapi@3.1", Path: "spec.yaml"}

	c := service.New(0, &load.Document{Doc: openapitest.DocDeclaring(), Source: src}, service.Policy{})

	assert.Equal(t, []ir.SourceInfo{src}, c.Sources())
}

// TestPlace_NamesTheOverlayForThePositionsItIntroduced pins the one place
// a source index is spelled into a Provenance. Asking the overlay here is what
// makes an overlay's contribution traceable without touching a lowering, so a
// Place that ignored it would credit the overlay's work to the file on disk at
// every site at once. The position is the source's either way.
func TestPlace_NamesTheOverlayForThePositionsItIntroduced(t *testing.T) {
	t.Parallel()
	origin := appliedOverlay(t, 1, "info: {title: T}\n",
		"overlay: 1.0.0\ninfo: {title: O, version: \"1\"}\nactions:\n"+
			"  - target: $.info\n    update: {description: d}\n")

	c := service.New(0, &load.Document{Doc: openapitest.DocDeclaring(), Overlay: origin}, service.Policy{})

	assert.Equal(t, ir.Provenance{Source: 0, Pointer: "/info/description", WrittenBy: 1},
		c.Place().At("/info/description"), "the overlay wrote this position of the source")
	assert.Equal(t, ir.Provenance{Source: 0, Pointer: "/info/title"},
		c.Place().At("/info/title"), "and left this one alone")
	assert.Equal(t, ir.Provenance{Source: 0, Pointer: "/info/description", WrittenBy: 1},
		c.DiagAt(ir.SeverityWarning, "x", "/info/description", "m").Provenance,
		"a diagnostic is stamped through the same question")
}

// overlayDoc reads ov as the compile reads an overlay document, into the tree
// overlay.Apply is handed.
func overlayDoc(t testing.TB, ov string) *yaml.Node {
	t.Helper()
	doc, err := doctree.Read(t.Context(), []byte(ov), budget.Unbounded())
	require.NoError(t, err)
	return doc.Root
}
