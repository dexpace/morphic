// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package lowering_test

import (
	"encoding/json/jsontext"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/speakeasy-api/openapi/jsonschema/oas3"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/anchors"
	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/load"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/overlay"
	"github.com/dexpace/morphic/compilers/openapi/internal/resolve"
	"github.com/dexpace/morphic/engine/reader"
	"github.com/dexpace/morphic/ir"
)

// TestCtx_HasNoExportedMap holds Ctx to "immutable by value": a struct copy
// shares a map, so an exported map field would be the one part of the context a
// callee could write to, visibly to its caller's caller.
//
// Slices are held to the same rule, since a copy shares the backing array. The
// exported document pointer is exempt: it is shared by design and lowering
// never writes through it, which TestNew_KeepsTheDocumentItWasGiven pins.
func TestCtx_HasNoExportedMap(t *testing.T) {
	t.Parallel()
	rt := reflect.TypeFor[lowering.Ctx]()
	require.Positive(t, rt.NumField(), "a context with no fields would pass this vacuously")

	var checked int
	for f := range rt.Fields() {
		if !f.IsExported() {
			continue
		}
		checked++
		assert.NotEqual(t, reflect.Map, f.Type.Kind(),
			"exported field %s is a map, so a copy of the context shares it", f.Name)
		assert.NotEqual(t, reflect.Slice, f.Type.Kind(),
			"exported field %s is a slice, so a copy of the context shares its backing array", f.Name)
	}
	assert.Positive(t, checked, "no exported field was examined, so this asserted nothing")
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
		{
			name: "no document at all", doc: nil, denies: []string{"User", ""},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := lowering.New(0, tc.doc, ir.SourceInfo{}, "", lowering.Limits{}, lowering.StreamingMedia{}, lowering.ExtensionPromotions{}, lowering.NullableWidening{}, overlay.Origin{})
			for _, n := range tc.declares {
				assert.True(t, c.DeclaresSchema(n), "%q is declared", n)
			}
			for _, n := range tc.denies {
				assert.False(t, c.DeclaresSchema(n), "%q is not declared", n)
			}
		})
	}
}

// TestDeclaresSchema_TheZeroContextDeclaresNothing pins the nil-index read. The
// index is nil for a document with no components, so the accessor must answer
// on a nil map rather than assume one was built.
func TestDeclaresSchema_TheZeroContextDeclaresNothing(t *testing.T) {
	t.Parallel()
	var c lowering.Ctx
	assert.False(t, c.DeclaresSchema("User"))
	assert.False(t, c.DeclaresSchema(""))
}

// TestNew_KeepsTheDocumentItWasGiven pins the rest of the context: the document,
// grouping policy, source identity and index arrive unchanged, since every
// Provenance the compile stamps is built from the last two.
func TestNew_KeepsTheDocumentItWasGiven(t *testing.T) {
	t.Parallel()
	doc := openapitest.DocDeclaring("User")
	src := ir.SourceInfo{Format: "openapi@3.1", Path: "spec.yaml", Hash: "abc"}

	c := lowering.New(7, doc, src, lowering.GroupByPathPrefix, lowering.Limits{}, lowering.StreamingMedia{}, lowering.ExtensionPromotions{}, lowering.NullableWidening{}, overlay.Origin{})

	assert.Same(t, doc, c.Doc, "the document is referenced, never copied")
	assert.Equal(t, src, c.Source)
	assert.Equal(t, lowering.GroupByPathPrefix, c.Grouping)
	assert.Equal(t, 7, c.SrcIndex)
}

// TestIs30_TheZeroContextReadsAs2020 pins that a context with no document
// answers false rather than panicking: the accessor reaches through the
// document pointer, and half this file drives the type as a Ctx literal.
func TestIs30_TheZeroContextReadsAs2020(t *testing.T) {
	t.Parallel()
	var c lowering.Ctx
	assert.False(t, c.Is30(),
		"no document names no dialect, which falls back to 2020-12")
}

// TestIs30_FollowsTheDeclaredVersion pins which documents read as OpenAPI 3.0.
// A reading keyed on this answer goes wrong with it: reading 3.0 as 2020-12
// turns a boolean exclusiveMinimum into a numeric bound and silences a missing
// `items`, and the reverse warns on every 3.1 array that omits `items`.
func TestIs30_FollowsTheDeclaredVersion(t *testing.T) {
	t.Parallel()
	tests := []struct {
		version string
		want    bool
	}{
		{version: "3.0.0", want: true},
		{version: "3.0.3", want: true},
		{version: "3.1.0", want: false},
		{version: "3.2.0", want: false},
		{version: "4.0.0", want: false},
		{version: "", want: false},
	}
	for _, tc := range tests {
		t.Run(tc.version, func(t *testing.T) {
			t.Parallel()
			c := lowering.New(0, &soa.OpenAPI{OpenAPI: tc.version}, ir.SourceInfo{}, "", lowering.Limits{}, lowering.StreamingMedia{}, lowering.ExtensionPromotions{}, lowering.NullableWidening{}, overlay.Origin{})
			assert.Equal(t, tc.want, c.Is30())
		})
	}
}

// TestReached_IsSharedByTheContextAndItsScope pins where the record of resolved
// references lives: a context and the scope it builds hold the same one, so a
// resolution through any copy is visible to the compiler, and a zero context,
// which holds none, has reached nothing.
func TestReached_IsSharedByTheContextAndItsScope(t *testing.T) {
	t.Parallel()
	c := lowering.New(0, &soa.OpenAPI{}, ir.SourceInfo{}, "", lowering.Limits{}, lowering.StreamingMedia{},
		lowering.ExtensionPromotions{}, lowering.NullableWidening{}, overlay.Origin{})
	require.NotNil(t, c.RefScope().Reached)
	assert.Same(t, c.RefScope().Reached, c.InScope(c.RefScope()).RefScope().Reached, "copies share one record")
	assert.False(t, c.Reached("/components/parameters/P"), "nothing has been resolved yet")
	assert.False(t, lowering.Ctx{}.Reached("/components/parameters/P"))
	assert.Nil(t, lowering.Ctx{}.RefScope().Reached)
}

// TestKeptOnce_ReadsADeclarationOnce holds what makes a kept entry cost what the
// source wrote: the first reach reads and reports, a later one through any copy of
// the context does neither, and another declaration is a read of its own.
func TestKeptOnce_ReadsADeclarationOnce(t *testing.T) {
	t.Parallel()
	c := lowering.New(0, &soa.OpenAPI{}, ir.SourceInfo{}, "", lowering.Limits{}, lowering.StreamingMedia{},
		lowering.ExtensionPromotions{}, lowering.NullableWidening{}, overlay.Origin{})
	reads := 0
	keep := func(key string) func() (ir.Unmodeled, []ir.Diagnostic) {
		return func() (ir.Unmodeled, []ir.Diagnostic) {
			reads++
			return ir.Unmodeled{key: {Reason: ir.ReasonDegradedLowering}}, []ir.Diagnostic{{Code: key}}
		}
	}

	assert.Len(t, c.Kept.Once("/a", keep("a")), 1, "the first reach reports")
	assert.Empty(t, c.InScope(c.RefScope()).Kept.Once("/a", keep("a")), "a copy shares what was read")
	assert.Len(t, c.Kept.Once("/b", keep("b")), 1)

	assert.Equal(t, 2, reads)
	assert.Equal(t, []string{"a", "b"}, slices.Sorted(maps.Keys(c.Kept.Entries())))
}

// TestKeptOnce_AZeroContextKeepsNothing is the state New never leaves: with nowhere
// to hold a read, it asks for none rather than report what nothing kept.
func TestKeptOnce_AZeroContextKeepsNothing(t *testing.T) {
	t.Parallel()
	asked := false
	diags := lowering.Ctx{}.Kept.Once("/a", func() (ir.Unmodeled, []ir.Diagnostic) {
		asked = true
		return nil, nil
	})
	assert.False(t, asked)
	assert.Empty(t, diags)
	assert.Nil(t, lowering.Ctx{}.Kept.Entries())
}

// TestRefScope_IsTheContextSeenAsAScope pins the two facts reference resolution
// reads, and that both come from the context rather than from a copy beside it:
// the document's own path decides internal from external, and the declared set
// decides whether an internal pointer names anything.
func TestRefScope_IsTheContextSeenAsAScope(t *testing.T) {
	t.Parallel()
	c := lowering.New(0, openapitest.DocDeclaring("User"), ir.SourceInfo{Path: "spec.yaml"}, "", lowering.Limits{}, lowering.StreamingMedia{}, lowering.ExtensionPromotions{}, lowering.NullableWidening{}, overlay.Origin{})

	scope := c.RefScope()

	assert.Equal(t, "spec.yaml", scope.SelfPath)
	require.NotNil(t, scope.Declares, "a scope with no predicate would resolve nothing")
	assert.True(t, scope.Declares("User"))
	assert.False(t, scope.Declares("Missing"))
}

// TestRefScope_NoDocumentResolvesNoRef pins that a context with no document
// hands over a scope with none, rather than one holding a nil pointer: that
// passes every nil check a reader makes and then faults when a pointer is
// navigated in it. With no load result it holds no bindings either, so no
// schema $ref lands anywhere.
func TestRefScope_NoDocumentResolvesNoRef(t *testing.T) {
	t.Parallel()
	scope := lowering.Ctx{}.RefScope()

	assert.Equal(t, nil, scope.Doc, "a nil interface, not one holding a nil pointer")
	assert.Nil(t, scope.Bindings)
	_, ok := scope.Target(&oas3.JSONSchema[oas3.Referenceable]{})
	assert.False(t, ok, "no bindings to say where a $ref lands")
}

// bindingsSpec declares a component schema whose $ref lands on another, for
// the tests that follow the bindings a context carries.
const bindingsSpec = `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
components:
  schemas:
    Target: {type: string}
    Alias: {$ref: '#/components/schemas/Target'}
`

// TestRefScope_CarriesTheBindingsItWasGiven pins that the document's schema
// bindings reach the scope through the context, and only through WithLoaded:
// the context it was derived from and a zero context hold none, so no $ref
// lands there. Every copy of the carrying context shares the one set, since they
// are built once and never written, and a $ref lands where the load phase
// resolved it.
func TestRefScope_CarriesTheBindingsItWasGiven(t *testing.T) {
	t.Parallel()
	loaded, _, err := load.Load(t.Context(), 0, compilers.Source{Path: "spec.yaml", Data: []byte(bindingsSpec)}, load.Options{})
	require.NoError(t, err)
	require.NotNil(t, loaded)
	require.NotNil(t, loaded.Bindings, "the load phase bound the source")
	alias, ok := loaded.Doc.Components.Schemas.Get("Alias")
	require.True(t, ok)

	bare := lowering.New(0, loaded.Doc, loaded.Source, "", lowering.Limits{}, lowering.StreamingMedia{}, lowering.ExtensionPromotions{}, lowering.NullableWidening{}, loaded.Overlay)
	assert.Nil(t, bare.Bindings(), "a context given no load result knows no binding")
	assert.Nil(t, bare.RefScope().Bindings)
	_, ok = bare.RefScope().Target(alias)
	assert.False(t, ok, "so its $ref lands nowhere")

	carrying := bare.WithLoaded(loaded)
	assert.Same(t, loaded.Bindings, carrying.Bindings())
	assert.Same(t, loaded.Bindings, carrying.RefScope().Bindings)
	assert.Same(t, loaded.Bindings, carrying.InScope(carrying.RefScope()).Bindings(), "a copy shares it")
	at, ok := carrying.RefScope().Target(alias)
	require.True(t, ok, "its $ref lands where the load phase resolved it")
	assert.Equal(t, bind.Position{Pointer: "/components/schemas/Target"}, at)

	assert.Nil(t, bare.Bindings(), "the context it was derived from still has none")
	assert.Nil(t, lowering.Ctx{}.Bindings(), "and a zero context never had any")
}

// TestRefScope_ReadsAMappingTargetTheModelHoldsAsRawYAML pins that a mapping
// target the load phase resolved reaches the scope through the context. The
// target is an extension's value, a position the parsed model holds as raw
// YAML, so the scope finds a schema there only if WithLoaded handed over the
// bindings, which built one (GitHub #757). The scope asks the load phase's
// index, too, what a mapping a walk leaves the model for holds
// (resolve.Scope.Holds).
func TestRefScope_ReadsAMappingTargetTheModelHoldsAsRawYAML(t *testing.T) {
	t.Parallel()
	const spec = `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
components:
  schemas:
    Pet:
      type: object
      discriminator: {propertyName: k, mapping: {c: '#/x-lib/Cat'}}
x-lib:
  Cat: {type: object}
`
	loaded, _, err := load.Load(t.Context(), 0, compilers.Source{Path: "spec.yaml", Data: []byte(spec)}, load.Options{})
	require.NoError(t, err)
	require.NotNil(t, loaded)
	pet, ok := loaded.Doc.Components.Schemas.Get("Pet")
	require.True(t, ok)
	d := pet.GetSchema().GetDiscriminator()
	require.NotNil(t, d)

	bare := lowering.New(0, loaded.Doc, loaded.Source, "", lowering.Limits{}, lowering.StreamingMedia{}, lowering.ExtensionPromotions{}, lowering.NullableWidening{}, loaded.Overlay)
	_, _, ok = bare.RefScope().MappingTarget(d, "#/x-lib/Cat")
	assert.False(t, ok, "so a context without the bindings finds no target")

	carrying := bare.WithLoaded(loaded)
	at, built, ok := carrying.RefScope().MappingTarget(d, "#/x-lib/Cat")
	require.True(t, ok)
	assert.Equal(t, bind.Position{Pointer: "/x-lib/Cat"}, at)
	require.NotNil(t, built, "the load phase built a schema there")
	assert.Equal(t, oas3.SchemaTypeObject, built.GetSchema().GetType()[0])
	_, _, ok = bare.RefScope().MappingTarget(d, "#/x-lib/Cat")
	assert.False(t, ok, "the context it was derived from still finds none")

	holds := carrying.RefScope().Holds
	require.NotNil(t, holds)
	assert.True(t, holds(loaded.Doc.GetRootNode(), "x-lib"))
	assert.False(t, holds(loaded.Doc.GetRootNode(), "x-none"))
}

// TestRemembered_IsReadOnceAcrossCopies pins that a reading is read once for every
// copy of a context, that WithLoaded starts afresh because a reading turns on what
// it loaded, and that a context with nothing loaded reads every time.
func TestRemembered_IsReadOnceAcrossCopies(t *testing.T) {
	t.Parallel()
	reads := 0
	read := func() int {
		reads++
		return reads
	}
	c := lowering.Ctx{}.WithLoaded(&load.Document{})

	assert.Equal(t, 1, lowering.Remembered(c, "k", read))
	assert.Equal(t, 1, lowering.Remembered(c.InScope(c.RefScope().InSource()), "k", read), "a copy shares what c read")
	assert.Equal(t, 2, lowering.Remembered(c, "other", read), "another key is another reading")
	assert.Equal(t, 3, lowering.Remembered(c.WithLoaded(&load.Document{}), "k", read), "WithLoaded remembers nothing")

	var zero lowering.Ctx
	assert.Equal(t, 4, lowering.Remembered(zero, "k", read))
	assert.Equal(t, 5, lowering.Remembered(zero, "k", read), "a context with nothing loaded remembers nothing")
}

// TestRemembered_AKeyHoldsOneTypeOfAnswer pins a key misused by two kinds of
// reading: the answer is read again rather than taken for the wrong type.
func TestRemembered_AKeyHoldsOneTypeOfAnswer(t *testing.T) {
	t.Parallel()
	c := lowering.Ctx{}.WithLoaded(&load.Document{})

	assert.Equal(t, 1, lowering.Remembered(c, "k", func() int { return 1 }))
	assert.Equal(t, "s", lowering.Remembered(c, "k", func() string { return "s" }))
}

// TestTypePosition_IsReadOncePerScope pins the answers every copy of a context
// shares: a position is read once in each scope it is read in, and a copy
// reads what another remembered. A copy carrying other
// bindings starts afresh, since an answer reads them, and a zero context
// remembers nothing.
func TestTypePosition_IsReadOncePerScope(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "ext.yaml"),
		[]byte("paths:\n  /x: {get: {responses: {\"200\": {description: ok}}}}\n"), 0o600))
	const root = "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths:\n  /ext: {$ref: './ext.yaml#/paths/~1x'}\n"
	doc, _, err := load.Load(t.Context(), 0, compilers.Source{Path: filepath.Join(dir, "root.yaml"),
		Data: []byte(root)}, load.Options{Reader: reader.Disk{}})
	require.NoError(t, err)
	c := lowering.New(0, doc.Doc, doc.Source, "", lowering.Limits{}, lowering.StreamingMedia{},
		lowering.ExtensionPromotions{}, lowering.NullableWidening{}, overlay.Origin{})
	reads := 0
	ask := func(c lowering.Ctx, pointer jsontext.Pointer) {
		t.Helper()
		at := c.TypePosition(bind.Position{Pointer: pointer}, func() bind.Position {
			reads++
			return bind.Position{Doc: 1, Pointer: pointer + "/there"}
		})
		assert.Equal(t, bind.Position{Doc: 1, Pointer: pointer + "/there"}, at)
	}

	ask(c, "/x")
	ask(c.InScope(c.RefScope()), "/x")
	assert.Equal(t, 1, reads, "a copy reads what the context remembered")
	ask(c, "/y")
	ask(c, "/y")
	assert.Equal(t, 2, reads, "another position is read once too")
	ext, ok := doc.Doc.Paths.Get("/ext")
	require.True(t, ok)
	foreign := lowering.Within[soa.PathItem](c, ext)
	require.True(t, foreign.RefScope().Foreign)
	ask(foreign, "/x")
	assert.Equal(t, 3, reads, "the position is read again in another document's scope")
	ask(c.WithLoaded(&load.Document{}), "/x")
	assert.Equal(t, 4, reads, "a copy carrying other bindings remembers nothing of c's")
	ask(lowering.Ctx{}, "/x")
	ask(lowering.Ctx{}, "/x")
	assert.Equal(t, 6, reads, "a zero context remembers nothing")
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

// TestRefScope_EndsReadsEachReferenceKind pins the dispatch a walk through the
// document asks about each node it passes: every kind the library models as a
// reference is read as one, so a pointer passing an alias of any kind is read
// where that alias's chain ends. A kind left out would pass unread. An inline
// entry, and a value of no reference kind, are no reference.
func TestRefScope_EndsReadsEachReferenceKind(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "root.yaml")
	doc, _, err := load.Load(t.Context(), 0, compilers.Source{Path: path, Data: []byte(referenceKinds)}, load.Options{})
	require.NoError(t, err)
	c := lowering.New(0, doc.Doc, doc.Source, "", lowering.Limits{}, lowering.StreamingMedia{},
		lowering.ExtensionPromotions{}, lowering.NullableWidening{}, overlay.Origin{})
	ends := c.RefScope().Ends
	require.NotNil(t, ends)
	cs := doc.Doc.Components
	for kind, entries := range map[string][2]any{
		"pathItems":       {cs.PathItems.GetOrZero("A"), cs.PathItems.GetOrZero("P")},
		"parameters":      {cs.Parameters.GetOrZero("A"), cs.Parameters.GetOrZero("P")},
		"headers":         {cs.Headers.GetOrZero("A"), cs.Headers.GetOrZero("P")},
		"requestBodies":   {cs.RequestBodies.GetOrZero("A"), cs.RequestBodies.GetOrZero("P")},
		"responses":       {cs.Responses.GetOrZero("A"), cs.Responses.GetOrZero("P")},
		"examples":        {cs.Examples.GetOrZero("A"), cs.Examples.GetOrZero("P")},
		"links":           {cs.Links.GetOrZero("A"), cs.Links.GetOrZero("P")},
		"callbacks":       {cs.Callbacks.GetOrZero("A"), cs.Callbacks.GetOrZero("P")},
		"securitySchemes": {cs.SecuritySchemes.GetOrZero("A"), cs.SecuritySchemes.GetOrZero("P")},
	} {
		end, ok := ends(entries[0])
		require.True(t, ok, "%s: the alias is read as a reference", kind)
		assert.Equal(t, resolve.End{Document: any(doc.Doc), Path: path,
			Pointer: jsontext.Pointer("/components/" + kind + "/P")}, end, kind)
		_, ok = ends(entries[1])
		assert.False(t, ok, "%s: an inline entry is no reference", kind)
	}
	_, ok := ends(doc.Doc)
	assert.False(t, ok, "a value of no reference kind is no reference")
}

// TestProvenanceAt_IsTheOnlyPlaceASourceIndexIsSpelled pins the guarantee
// GitHub #86 exists for. Provenance built by hand is how a diagnostic shipped
// with none (#43) — and a source index written wrong misattributes a node just
// as silently, since nothing downstream can tell a wrong one from a right one.
func TestProvenanceAt_IsTheOnlyPlaceASourceIndexIsSpelled(t *testing.T) {
	t.Parallel()
	c := lowering.Ctx{SrcIndex: 7}

	assert.Equal(t, ir.Provenance{Source: 7, Pointer: "/components/schemas/User"},
		c.ProvenanceAt("/components/schemas/User"))
	assert.Equal(t, ir.Provenance{Source: 7}, c.ProvenanceAt(""),
		"a document-level position carries the index and no pointer")
}

// TestDiagAt_StampsAndHandsBack pins the split the Tier-1 conversion needs: the
// diagnostic arrives already located, and arrives as a value. A lowering with no
// accumulator can therefore still be sure of its provenance, which is what the
// converted leaves could not be while only the accumulating form existed.
func TestDiagAt_StampsAndHandsBack(t *testing.T) {
	t.Parallel()
	c := lowering.Ctx{SrcIndex: 3}

	got := c.DiagAt(ir.SeverityWarning, diag.DegradedConstruct, "/paths/~1x", "dropped %d of %d", 1, 2)

	assert.Equal(t, ir.SeverityWarning, got.Severity)
	assert.Equal(t, diag.DegradedConstruct, got.Code)
	assert.Equal(t, ir.Provenance{Source: 3, Pointer: "/paths/~1x"}, got.Provenance)
	assert.Equal(t, "dropped 1 of 2", got.Message, "the format arguments are applied")
}

// appliedOverlay returns a real Origin by applying ov to a tree of doc. Origin's
// fields are unexported, so a context carrying one can only be built the way the
// loader builds it — which is also what keeps this test honest about the shape
// the compiler actually hands over.
func appliedOverlay(t *testing.T, index int, doc, ov string) overlay.Origin {
	t.Helper()
	var root yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte(doc), &root))

	origin, diags := overlay.Apply(t.Context(), index, &root, overlay.Options{Path: "patch.yaml", Data: []byte(ov)})
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

	c := lowering.New(0, openapitest.DocDeclaring(), src, "", lowering.Limits{}, lowering.StreamingMedia{}, lowering.ExtensionPromotions{}, lowering.NullableWidening{}, origin)

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

	c := lowering.New(0, openapitest.DocDeclaring(), src, "", lowering.Limits{}, lowering.StreamingMedia{}, lowering.ExtensionPromotions{}, lowering.NullableWidening{}, overlay.Origin{})

	assert.Equal(t, []ir.SourceInfo{src}, c.Sources())
}

// TestProvenanceAt_NamesTheOverlayForThePositionsItIntroduced pins the one place
// a source index is spelled into a Provenance. Asking the overlay here is what
// makes an overlay's contribution traceable without touching a lowering, so a
// ProvenanceAt that ignored it would attribute the overlay's work to the file on
// disk at every site at once.
func TestProvenanceAt_NamesTheOverlayForThePositionsItIntroduced(t *testing.T) {
	t.Parallel()
	origin := appliedOverlay(t, 1, "info: {title: T}\n",
		"overlay: 1.0.0\ninfo: {title: O, version: \"1\"}\nactions:\n"+
			"  - target: $.info\n    update: {description: d}\n")

	c := lowering.New(0, openapitest.DocDeclaring(), ir.SourceInfo{}, "", lowering.Limits{}, lowering.StreamingMedia{}, lowering.ExtensionPromotions{}, lowering.NullableWidening{}, origin)

	assert.Equal(t, ir.Provenance{Source: 1, Pointer: "/info/description"},
		c.ProvenanceAt("/info/description"), "the overlay introduced this position")
	assert.Equal(t, ir.Provenance{Source: 0, Pointer: "/info/title"},
		c.ProvenanceAt("/info/title"), "and left this one alone")
	assert.Equal(t, ir.Provenance{Source: 1, Pointer: "/info/description"},
		c.DiagAt(ir.SeverityWarning, "x", "/info/description", "m").Provenance,
		"a diagnostic is stamped through the same question")
}

// TestWithLoaded_CarriesTheSiblingsOnACopy holds where a lowering reads the
// fields beside a path item's $ref: from the load phase's model, carried on the
// copy WithLoaded returns, while the context it was made from holds none.
func TestWithLoaded_CarriesTheSiblingsOnACopy(t *testing.T) {
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
	base := lowering.New(0, doc.Doc, doc.Source, "", lowering.Limits{}, lowering.StreamingMedia{},
		lowering.ExtensionPromotions{}, lowering.NullableWidening{}, overlay.Origin{})
	carrying := base.WithLoaded(doc)
	rp, ok := doc.Doc.GetPaths().Get("/a")
	require.True(t, ok)

	sib, ok := carrying.Siblings().Of(rp)
	require.True(t, ok, "the copy reads the load phase's model")
	assert.NotNil(t, sib.Item.Put(), "the operation beside the $ref is modelled")
	assert.Equal(t, jsontext.Pointer("/paths/~1a"), sib.Pointer)
	assert.False(t, carrying.Siblings().Unread(rp), "a source use site is read")

	_, ok = base.Siblings().Of(rp)
	assert.False(t, ok, "the context it was made from is unchanged")
}

// TestWithLoaded_ReadsRawYAMLThroughTheLoadIndex pins that a context carrying
// the load phase's results reads the raw YAML a walk leaves the model for
// through an index, where the one it was made from asks the library, which
// scans a mapping on every read (GitHub #778).
func TestWithLoaded_ReadsRawYAMLThroughTheLoadIndex(t *testing.T) {
	t.Parallel()
	doc, diags, err := load.Load(t.Context(), 0, compilers.Source{Path: "spec.yaml", Data: []byte(openapitest.ComponentSpec("    A: {type: string}\n"))}, load.Options{})
	require.NoError(t, err)
	openapitest.RequireNoErrorDiags(t, diags)
	base := lowering.New(0, doc.Doc, doc.Source, "", lowering.Limits{}, lowering.StreamingMedia{},
		lowering.ExtensionPromotions{}, lowering.NullableWidening{}, overlay.Origin{})

	assert.Nil(t, base.RefScope().Holds)
	assert.NotNil(t, base.WithLoaded(doc).RefScope().Holds)
}

// TestDynamicAnchorSites_ReportsATruncatedWalkOnce pins what a reader is told
// when the $dynamicAnchor index is partial. Truncation only ever drops
// declarations, and a dropped duplicate reads as "declared exactly once", the
// count that expands, so an unreported truncation would turn an ambiguous
// reference into a confident one. The index is built once for every copy of
// the context, so the warning is too.
func TestDynamicAnchorSites_ReportsATruncatedWalkOnce(t *testing.T) {
	t.Parallel()
	// The walk descends into every key, so an extension at the document root
	// nests the tree past the cap without any schema lowering seeing it.
	deep := strings.Repeat("{a: ", anchors.MaxDepth) + "1" + strings.Repeat("}", anchors.MaxDepth)
	src := openapitest.ComponentSpec("    A: {$dynamicAnchor: m, type: string}\n") + "x-deep: " + deep + "\n"
	doc, diags, err := load.Load(t.Context(), 0, compilers.Source{Path: "spec.yaml", Data: []byte(src)}, load.Options{})
	require.NoError(t, err)
	openapitest.RequireNoErrorDiags(t, diags)
	c := lowering.New(0, doc.Doc, doc.Source, "", lowering.Limits{}, lowering.StreamingMedia{},
		lowering.ExtensionPromotions{}, lowering.NullableWidening{}, overlay.Origin{}).WithLoaded(doc)

	sites, got := c.DynamicAnchorSites("m")
	assert.Equal(t, []jsontext.Pointer{"/components/schemas/A"}, sites, "the index is built even when partial")
	require.Len(t, got, 1, "the truncation is announced: %+v", got)
	assert.Equal(t, ir.SeverityWarning, got[0].Severity)
	assert.Contains(t, got[0].Message, "not verified")

	_, again := c.InScope(c.RefScope()).DynamicAnchorSites("absent")
	assert.Empty(t, again, "a copy reads the built index and does not announce it again")

	bare := lowering.Ctx{Doc: doc.Doc}
	_, first := bare.DynamicAnchorSites("m")
	_, second := bare.DynamicAnchorSites("m")
	assert.Len(t, first, 1, "a context New did not build keeps no index")
	assert.Len(t, second, 1, "so it builds one, and announces it, on every ask")
}

// TestDynamicAnchorSites_ReadsACompleteIndexQuietly pins the ordinary case: a
// document the walk reads whole names each anchor's sites and reports
// nothing, and a context holding no document finds none.
func TestDynamicAnchorSites_ReadsACompleteIndexQuietly(t *testing.T) {
	t.Parallel()
	src := openapitest.ComponentSpec("    A: {$dynamicAnchor: m, type: string}\n    B: {$dynamicAnchor: m}\n")
	doc, diags, err := load.Load(t.Context(), 0, compilers.Source{Path: "spec.yaml", Data: []byte(src)}, load.Options{})
	require.NoError(t, err)
	openapitest.RequireNoErrorDiags(t, diags)
	c := lowering.New(0, doc.Doc, doc.Source, "", lowering.Limits{}, lowering.StreamingMedia{},
		lowering.ExtensionPromotions{}, lowering.NullableWidening{}, overlay.Origin{})

	sites, got := c.DynamicAnchorSites("m")
	assert.Equal(t, []jsontext.Pointer{"/components/schemas/A", "/components/schemas/B"}, sites)
	assert.Empty(t, got)

	sites, got = lowering.Ctx{}.DynamicAnchorSites("m")
	assert.Empty(t, sites)
	assert.Empty(t, got)
}

// TestNew_CarriesTheLimits pins that the budgets reach the walk through the
// context, beside the other policy the caller chose.
func TestNew_CarriesTheLimits(t *testing.T) {
	t.Parallel()
	limits := lowering.Limits{MaxEnumMembers: budget.AtMost(12)}

	c := lowering.New(0, openapitest.DocDeclaring(), ir.SourceInfo{}, lowering.GroupByTags, limits, lowering.StreamingMedia{}, lowering.ExtensionPromotions{}, lowering.NullableWidening{}, overlay.Origin{})

	assert.Equal(t, limits, c.Limits)
}
