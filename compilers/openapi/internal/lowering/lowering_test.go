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
	"testing"

	"github.com/speakeasy-api/openapi/jsonschema/oas3"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/load"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering/loweringtest"
	"github.com/dexpace/morphic/compilers/openapi/internal/oas"
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
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := lowering.New(0, &load.Document{Doc: tc.doc}, lowering.Policy{})
			for _, n := range tc.declares {
				assert.True(t, c.DeclaresSchema(n), "%q is declared", n)
			}
			for _, n := range tc.denies {
				assert.False(t, c.DeclaresSchema(n), "%q is not declared", n)
			}
		})
	}
}

// TestNew_KeepsTheDocumentItWasGiven pins the rest of the context: the document,
// grouping policy, source identity and index arrive unchanged, since every
// Provenance the compile stamps is built from the last two.
func TestNew_KeepsTheDocumentItWasGiven(t *testing.T) {
	t.Parallel()
	doc := openapitest.DocDeclaring("User")
	src := ir.SourceInfo{Format: "openapi@3.1", Path: "spec.yaml", Hash: "abc"}

	c := lowering.New(7, &load.Document{Doc: doc, Source: src}, lowering.Policy{Grouping: lowering.GroupByPathPrefix})

	assert.Same(t, doc, c.Doc, "the document is referenced, never copied")
	assert.Equal(t, src, c.Source)
	assert.Equal(t, lowering.GroupByPathPrefix, c.Grouping)
	assert.Equal(t, 7, c.SrcIndex)
}

// TestSchemaRules_FollowsTheDeclaredVersion pins which rows a document's
// schemas are read with: the load phase's, at the version the source declares.
// A reading keyed on them goes wrong with them: reading 3.0 as 2020-12 turns a
// boolean exclusiveMinimum into a numeric bound and silences a missing `items`,
// and the reverse warns on every 3.1 array that omits `items`.
func TestSchemaRules_FollowsTheDeclaredVersion(t *testing.T) {
	t.Parallel()
	tests := []struct {
		version string
		v30     bool
	}{
		{version: "3.0.0", v30: true},
		{version: "3.0.3", v30: true},
		{version: "3.1.0"},
		{version: "3.2.0"},
	}
	for _, tc := range tests {
		t.Run(tc.version, func(t *testing.T) {
			t.Parallel()
			c := loweringtest.Context(t, "openapi: "+tc.version+"\ninfo: {title: T, version: \"1\"}\npaths: {}\n",
				lowering.Policy{})
			rules := c.SchemaRules()
			assert.Equal(t, tc.v30, rules.Defines("nullable"))
			assert.Equal(t, tc.v30, rules.Takes("exclusiveMinimum", oas.Boolean))
			assert.Equal(t, tc.v30, rules.RequiredWhen("items", "type", "array"))
			assert.Equal(t, !tc.v30, rules.Takes("exclusiveMinimum", oas.Number))
		})
	}
}

// TestReached_IsSharedByTheContextAndItsScope pins where the record of resolved
// references lives: a context and the scope it builds hold the same one, so a
// resolution through any copy is visible to the compiler.
func TestReached_IsSharedByTheContextAndItsScope(t *testing.T) {
	t.Parallel()
	c := loweringtest.Context(t, bindingsSpec, lowering.Policy{})
	require.NotNil(t, c.RefScope().Reached)
	assert.Same(t, c.RefScope().Reached, c.InScope(c.RefScope()).RefScope().Reached, "copies share one record")
	assert.False(t, c.Reached("/components/parameters/P"), "nothing has been resolved yet")
}

// TestKeptOnce_ReadsADeclarationOnce holds what makes a kept entry cost what the
// source wrote: the first reach reads and reports, a later one through any copy of
// the context does neither, and another declaration is a read of its own.
func TestKeptOnce_ReadsADeclarationOnce(t *testing.T) {
	t.Parallel()
	c := loweringtest.Context(t, bindingsSpec, lowering.Policy{})
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

// TestRefScope_IsTheContextSeenAsAScope pins the two facts reference resolution
// reads, and that both come from the context rather than from a copy beside it:
// the document's own path decides internal from external, and the declared set
// decides whether an internal pointer names anything.
func TestRefScope_IsTheContextSeenAsAScope(t *testing.T) {
	t.Parallel()
	c := loweringtest.Context(t, openapitest.ComponentSpec("    User: {type: object}\n"), lowering.Policy{})

	scope := c.RefScope()

	assert.Equal(t, "spec.yaml", scope.SelfPath)
	assert.True(t, scope.Declares.Has("User"))
	assert.False(t, scope.Declares.Has("Missing"))
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
// bindings reach the scope through the context. Every copy of the context
// shares the one set, since they are built once and never written, and a $ref
// lands where the load phase resolved it.
func TestRefScope_CarriesTheBindingsItWasGiven(t *testing.T) {
	t.Parallel()
	loaded, _, err := load.Load(t.Context(), 0, compilers.Source{Path: "spec.yaml", Data: []byte(bindingsSpec)}, load.Options{})
	require.NoError(t, err)
	require.NotNil(t, loaded)
	require.NotNil(t, loaded.Bindings, "the load phase bound the source")
	alias, ok := loaded.Doc.Components.Schemas.Get("Alias")
	require.True(t, ok)

	carrying := lowering.New(0, loaded, lowering.Policy{})
	assert.Same(t, loaded.Bindings, carrying.Bindings())
	assert.Same(t, loaded.Bindings, carrying.RefScope().Bindings)
	assert.Same(t, loaded.Bindings, carrying.InScope(carrying.RefScope()).Bindings(), "a copy shares it")
	at, ok := carrying.RefScope().Target(alias)
	require.True(t, ok, "its $ref lands where the load phase resolved it")
	assert.Equal(t, bind.Position{Pointer: "/components/schemas/Target"}, at)
}

// TestRefScope_ReadsAMappingTargetTheModelHoldsAsRawYAML pins that a mapping
// target the load phase resolved reaches the scope through the context. The
// target is an extension's value, a position the parsed model holds as raw
// YAML, so the scope finds a schema there only through the bindings the load
// phase built, which built one (GitHub #757). The scope asks the load phase's
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
	c := loweringtest.Context(t, spec, lowering.Policy{})
	pet, ok := c.Doc.Components.Schemas.Get("Pet")
	require.True(t, ok)
	d := pet.GetSchema().GetDiscriminator()
	require.NotNil(t, d)

	at, built, ok := c.RefScope().MappingTarget(d, "#/x-lib/Cat")
	require.True(t, ok)
	assert.Equal(t, bind.Position{Pointer: "/x-lib/Cat"}, at)
	require.NotNil(t, built, "the load phase built a schema there")
	assert.Equal(t, oas3.SchemaTypeObject, built.GetSchema().GetType()[0])

	holds := c.RefScope().Holds
	require.NotNil(t, holds)
	assert.True(t, holds(c.Doc.GetRootNode(), "x-lib"))
	assert.False(t, holds(c.Doc.GetRootNode(), "x-none"))
}

// TestRemembered_IsReadOnceAcrossCopies pins that a reading is read once for every
// copy of a context, and afresh for another context, because a reading turns on
// what that one loaded.
func TestRemembered_IsReadOnceAcrossCopies(t *testing.T) {
	t.Parallel()
	reads := 0
	read := func() int {
		reads++
		return reads
	}
	c := loweringtest.Context(t, bindingsSpec, lowering.Policy{})

	assert.Equal(t, 1, lowering.Remembered(c, "k", read))
	assert.Equal(t, 1, lowering.Remembered(c.InScope(c.RefScope().InSource()), "k", read), "a copy shares what c read")
	assert.Equal(t, 2, lowering.Remembered(c, "other", read), "another key is another reading")
	other := loweringtest.Context(t, bindingsSpec, lowering.Policy{})
	assert.Equal(t, 3, lowering.Remembered(other, "k", read), "another context remembers nothing of c's")
}

// TestRemembered_ReadsAKeyOnceForEachTypeOfAnswer pins a key asked for two
// types of answer: each is read once, and neither is taken for the other.
func TestRemembered_ReadsAKeyOnceForEachTypeOfAnswer(t *testing.T) {
	t.Parallel()
	c := loweringtest.Context(t, bindingsSpec, lowering.Policy{})
	reads := 0
	number := func() int {
		reads++
		return 1
	}
	text := func() string {
		reads++
		return "s"
	}

	assert.Equal(t, 1, lowering.Remembered(c, "k", number))
	assert.Equal(t, "s", lowering.Remembered(c, "k", text))
	assert.Equal(t, 1, lowering.Remembered(c, "k", number))
	assert.Equal(t, "s", lowering.Remembered(c, "k", text))
	assert.Equal(t, 2, reads, "each type of answer is read once")
}

// TestRemembered_KeepsWhatAReadingRemembers pins a reading that asks Remembered
// for another key of its own types: the answer it remembered is kept beside
// its own.
func TestRemembered_KeepsWhatAReadingRemembers(t *testing.T) {
	t.Parallel()
	c := loweringtest.Context(t, bindingsSpec, lowering.Policy{})
	reads := 0
	inner := func() int {
		reads++
		return 1
	}
	outer := func() int {
		reads++
		return lowering.Remembered(c, "inner", inner) + 1
	}

	assert.Equal(t, 2, lowering.Remembered(c, "outer", outer))
	assert.Equal(t, 1, lowering.Remembered(c, "inner", inner))
	assert.Equal(t, 2, reads, "inner was read once, within outer")
}

// TestScoped_KeysAPositionByTheScopeItIsReadIn pins the key a position is
// remembered under: one for every copy of a context in its scope, and another
// for another position, for another document's scope, and for the scope of a
// chain cut short, which is no document's.
func TestScoped_KeysAPositionByTheScopeItIsReadIn(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "ext.yaml"),
		[]byte("paths:\n  /x: {get: {responses: {\"200\": {description: ok}}}}\n"), 0o600))
	const root = "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths:\n  /ext: {$ref: './ext.yaml#/paths/~1x'}\n"
	doc, _, err := load.Load(t.Context(), 0, compilers.Source{Path: filepath.Join(dir, "root.yaml"),
		Data: []byte(root)}, load.Options{Reader: reader.Disk{}})
	require.NoError(t, err)
	c := lowering.New(0, doc, lowering.Policy{})
	at := bind.Position{Pointer: "/x"}

	if lowering.Scoped(c, at) != lowering.Scoped(c.InScope(c.RefScope()), at) {
		t.Error("a copy in c's scope keys the position apart from c")
	}
	if lowering.Scoped(c, at) == lowering.Scoped(c, bind.Position{Pointer: "/y"}) {
		t.Error("another position shares the key")
	}
	ext, ok := doc.Doc.Paths.Get("/ext")
	require.True(t, ok)
	foreign := lowering.Within[soa.PathItem](c, ext)
	require.True(t, foreign.RefScope().Foreign)
	if lowering.Scoped(c, at) == lowering.Scoped(foreign, at) {
		t.Error("another document's scope shares the key")
	}

	const cycle = "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\n" +
		"paths:\n  /a: {$ref: '#/paths/~1b'}\n  /b: {$ref: '#/paths/~1a'}\n"
	inCycle := loweringtest.Context(t, cycle, lowering.Policy{})
	a, ok := inCycle.Doc.Paths.Get("/a")
	require.True(t, ok)
	cut := lowering.Within[soa.PathItem](inCycle, a)
	require.True(t, cut.RefScope().Foreign)
	require.Empty(t, cut.RefScope().Holder, "a cut chain is no document's")
	if lowering.Scoped(inCycle, at) == lowering.Scoped(cut, at) {
		t.Error("a cut chain's scope shares the key")
	}
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
	ends := lowering.New(0, doc, lowering.Policy{}).RefScope().Ends
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
	c := lowering.New(7, &load.Document{Doc: &soa.OpenAPI{}}, lowering.Policy{})

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
	c := lowering.New(3, &load.Document{Doc: &soa.OpenAPI{}}, lowering.Policy{})

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

	c := lowering.New(0, &load.Document{Doc: openapitest.DocDeclaring(), Source: src, Overlay: origin}, lowering.Policy{})

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

	c := lowering.New(0, &load.Document{Doc: openapitest.DocDeclaring(), Source: src}, lowering.Policy{})

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

	c := lowering.New(0, &load.Document{Doc: openapitest.DocDeclaring(), Overlay: origin}, lowering.Policy{})

	assert.Equal(t, ir.Provenance{Source: 1, Pointer: "/info/description"},
		c.ProvenanceAt("/info/description"), "the overlay introduced this position")
	assert.Equal(t, ir.Provenance{Source: 0, Pointer: "/info/title"},
		c.ProvenanceAt("/info/title"), "and left this one alone")
	assert.Equal(t, ir.Provenance{Source: 1, Pointer: "/info/description"},
		c.DiagAt(ir.SeverityWarning, "x", "/info/description", "m").Provenance,
		"a diagnostic is stamped through the same question")
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
	c := lowering.New(0, doc, lowering.Policy{})
	rp, ok := doc.Doc.GetPaths().Get("/a")
	require.True(t, ok)

	sib, ok := c.Siblings().Of(rp)
	require.True(t, ok, "the context reads the load phase's model")
	assert.NotNil(t, sib.Item.Put(), "the operation beside the $ref is modelled")
	assert.Equal(t, jsontext.Pointer("/paths/~1a"), sib.Pointer)
	assert.False(t, c.Siblings().Unread(rp), "a source use site is read")
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
	c := lowering.New(0, doc, lowering.Policy{})
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

// TestDynamicAnchorSites_ReadsTheBindingsOfItsDocument pins that the sites are
// the table's for the document c reads, every one of them counted.
func TestDynamicAnchorSites_ReadsTheBindingsOfItsDocument(t *testing.T) {
	t.Parallel()
	src := openapitest.ComponentSpec("    A: {$dynamicAnchor: m, type: string}\n    B: {$dynamicAnchor: m}\n")
	doc, diags, err := load.Load(t.Context(), 0, compilers.Source{Path: "spec.yaml", Data: []byte(src)}, load.Options{})
	require.NoError(t, err)
	openapitest.RequireNoErrorDiags(t, diags)
	c := lowering.New(0, doc, lowering.Policy{})

	sites, whole := c.DynamicAnchorSites("m")
	assert.Equal(t, []bind.Position{{Pointer: "/components/schemas/A"}, {Pointer: "/components/schemas/B"}}, sites)
	assert.True(t, whole)
}

// TestPosition_PlacesWhatADocumentHolds pins that a context reading the source,
// or a document at its own coordinates, places a pointer, and that one reading
// another document's content at the source's places nothing.
func TestPosition_PlacesWhatADocumentHolds(t *testing.T) {
	t.Parallel()
	c := loweringtest.Context(t, bindingsSpec, lowering.Policy{})
	p, placed := c.Position("/a")
	assert.True(t, placed)
	assert.Equal(t, bind.Position{Pointer: "/a"}, p)

	_, placed = c.InScope(resolve.Scope{Foreign: true, Holder: "other.yaml"}).Position("/a")
	assert.False(t, placed)
}

// TestNew_CarriesTheLimits pins that the budgets reach the walk through the
// context, beside the other policy the caller chose.
func TestNew_CarriesTheLimits(t *testing.T) {
	t.Parallel()
	limits := lowering.Limits{MaxEnumMembers: budget.AtMost(12)}

	c := loweringtest.Context(t, bindingsSpec, lowering.Policy{Grouping: lowering.GroupByTags, Limits: limits})

	assert.Equal(t, limits, c.Limits)
}
