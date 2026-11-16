// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package lowering_test

import (
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"github.com/speakeasy-api/openapi/jsonschema/oas3"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/load"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/oas"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/promotion"
	"github.com/dexpace/morphic/compilers/openapi/internal/provenance"
	"github.com/dexpace/morphic/compilers/openapi/internal/resolve"
	"github.com/dexpace/morphic/compilers/openapi/internal/service"
	"github.com/dexpace/morphic/compilers/openapi/internal/service/servicetest"
	"github.com/dexpace/morphic/compilers/openapi/internal/statement"
	"github.com/dexpace/morphic/ir"
)

// TestCtx_HasNoExportedMap holds Ctx to "immutable by value": a struct copy
// shares a map, so an exported map field would be the one part of the context a
// callee could write to, visibly to its caller's caller. Slices are held to the
// same rule, since a copy shares the backing array.
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
			c := servicetest.Context(t, "openapi: "+tc.version+"\ninfo: {title: T, version: \"1\"}\npaths: {}\n",
				service.Policy{})
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
	c := servicetest.Context(t, bindingsSpec, service.Policy{})
	require.NotNil(t, c.RefScope().Reached)
	assert.Same(t, c.RefScope().Reached, atSource(t, c.Ctx).RefScope().Reached, "copies share one record")
	assert.False(t, c.Reached("/components/parameters/P"), "nothing has been resolved yet")
}

// TestRefScope_IsTheContextSeenAsAScope pins the two facts reference resolution
// reads, and that both come from the context rather than from a copy beside it:
// the document's own path decides internal from external, and the declared set
// decides whether an internal pointer names anything.
func TestRefScope_IsTheContextSeenAsAScope(t *testing.T) {
	t.Parallel()
	c := servicetest.Context(t, openapitest.ComponentSpec("    User: {type: object}\n"), service.Policy{})

	scope := c.RefScope()

	assert.Equal(t, "spec.yaml", scope.SelfPath)
	assert.True(t, scope.Declares.Has("User"))
	assert.False(t, scope.Declares.Has("Missing"))
}

// TestRefScope_ReadsTheSourceWhereItLies pins that the scope judges a document
// part internal against where the source lies, its Location, which the
// references were read against, and not against the Path the caller handed,
// which Document.Sources records.
func TestRefScope_ReadsTheSourceWhereItLies(t *testing.T) {
	t.Parallel()
	located := filepath.Join(t.TempDir(), "spec.yaml")
	doc, _, err := load.Load(t.Context(), 0, compilers.Source{Path: "spec.yaml", Location: located,
		Data: []byte(openapitest.ComponentSpec("    User: {type: object}\n"))}, load.Options{})
	require.NoError(t, err)
	require.NotNil(t, doc)

	scope := service.New(0, doc, service.Policy{}).RefScope()

	assert.Equal(t, located, scope.SelfPath)
	_, internal := scope.InternalPointer(located + "#/components/schemas/User")
	assert.True(t, internal, "a $ref naming the source where it lies is internal")
	_, internal = scope.InternalPointer("spec.yaml#/components/schemas/User")
	assert.True(t, internal, "and so is one naming it relatively, read against where it lies")
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

	carrying := service.New(0, loaded, service.Policy{})
	assert.Same(t, loaded.Bindings, carrying.Bindings())
	assert.Same(t, loaded.Bindings, carrying.RefScope().Bindings)
	assert.Same(t, loaded.Bindings, atSource(t, carrying.Ctx).Bindings(), "a copy shares it")
	e, ok := carrying.RefScope().Bindings.Entry(alias)
	require.True(t, ok && e.Lands(), "its $ref lands where the load phase resolved it")
	assert.Equal(t, bind.Position{Pointer: "/components/schemas/Target"}, e.Hop.Target)
}

// TestRefScope_ReadsAMappingTargetTheModelHoldsAsRawYAML pins that a mapping
// target the load phase resolved reaches the scope through the context. The
// target is an extension's value, a position the parsed model holds as raw
// YAML, so the scope finds a schema there only through the bindings the load
// phase built, which built one (GitHub #757).
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
	c := servicetest.Context(t, spec, service.Policy{})
	pet, ok := c.Doc.Components.Schemas.Get("Pet")
	require.True(t, ok)
	require.NotNil(t, pet.GetSchema().GetDiscriminator())

	at, ok := c.RefScope().MappingTarget(bind.Position{Pointer: "/components/schemas/Pet/discriminator"}, "#/x-lib/Cat")
	require.True(t, ok)
	assert.Equal(t, bind.Position{Pointer: "/x-lib/Cat"}, at)
	built := c.Bindings().At(at)
	require.NotNil(t, built, "the load phase built a schema there")
	assert.Equal(t, oas3.SchemaTypeObject, built.GetSchema().GetType()[0])
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
	c := servicetest.Context(t, bindingsSpec, service.Policy{})

	assert.Equal(t, 1, lowering.Remembered(c.Ctx, "k", read))
	assert.Equal(t, 1, lowering.Remembered(atSource(t, c.Ctx), "k", read), "a copy shares what c read")
	assert.Equal(t, 2, lowering.Remembered(c.Ctx, "other", read), "another key is another reading")
	other := servicetest.Context(t, bindingsSpec, service.Policy{})
	assert.Equal(t, 3, lowering.Remembered(other.Ctx, "k", read), "another context remembers nothing of c's")
}

// TestStatements_IsOneTableForTheRun pins that every copy of a context reads
// statements from one table, over the bindings and rows the load phase read,
// and another context from its own.
func TestStatements_IsOneTableForTheRun(t *testing.T) {
	t.Parallel()
	c := servicetest.Context(t, bindingsSpec, service.Policy{})
	table := c.Statements()

	assert.Same(t, table, atSource(t, c.Ctx).Statements(), "a copy shares the table")
	target := table.At(c.Position("/components/schemas/Target"))
	require.NotNil(t, target.Kinds)
	assert.Equal(t, []statement.Kind{statement.KindString}, target.Kinds.Types, "read off the bindings")
	k, _ := target.Key("type")
	assert.Equal(t, oas.SlotKinds, k.Slot, "read in the rows the load phase read")
	other := servicetest.Context(t, bindingsSpec, service.Policy{})
	assert.NotSame(t, table, other.Statements(), "another run reads its own")
}

// TestRemembered_ReadsAKeyOnceForEachTypeOfAnswer pins a key asked for two
// types of answer: each is read once, and neither is taken for the other.
func TestRemembered_ReadsAKeyOnceForEachTypeOfAnswer(t *testing.T) {
	t.Parallel()
	c := servicetest.Context(t, bindingsSpec, service.Policy{})
	reads := 0
	number := func() int {
		reads++
		return 1
	}
	text := func() string {
		reads++
		return "s"
	}

	assert.Equal(t, 1, lowering.Remembered(c.Ctx, "k", number))
	assert.Equal(t, "s", lowering.Remembered(c.Ctx, "k", text))
	assert.Equal(t, 1, lowering.Remembered(c.Ctx, "k", number))
	assert.Equal(t, "s", lowering.Remembered(c.Ctx, "k", text))
	assert.Equal(t, 2, reads, "each type of answer is read once")
}

// TestRemembered_KeepsWhatAReadingRemembers pins a reading that asks Remembered
// for another key of its own types: the answer it remembered is kept beside
// its own.
func TestRemembered_KeepsWhatAReadingRemembers(t *testing.T) {
	t.Parallel()
	c := servicetest.Context(t, bindingsSpec, service.Policy{})
	reads := 0
	inner := func() int {
		reads++
		return 1
	}
	outer := func() int {
		reads++
		return lowering.Remembered(c.Ctx, "inner", inner) + 1
	}

	assert.Equal(t, 2, lowering.Remembered(c.Ctx, "outer", outer))
	assert.Equal(t, 1, lowering.Remembered(c.Ctx, "inner", inner))
	assert.Equal(t, 2, reads, "inner was read once, within outer")
}

// TestSettled_BuildsOneStorePerRun pins that a store is built once for every
// copy of a context, one for each type of store, and afresh for another
// context, whose settlements read another registry.
func TestSettled_BuildsOneStorePerRun(t *testing.T) {
	t.Parallel()
	type counts struct{ n int }
	type names struct{ s string }
	builds := 0
	build := func() *counts {
		builds++
		return &counts{n: builds}
	}
	c := servicetest.Context(t, bindingsSpec, service.Policy{})

	first := lowering.Settled(c.Ctx, build)
	assert.Same(t, first, lowering.Settled(atSource(t, c.Ctx), build), "a copy shares c's store")
	assert.Equal(t, &names{s: "n"}, lowering.Settled(c.Ctx, func() *names { return &names{s: "n"} }),
		"another type of store is another store")
	assert.Same(t, first, lowering.Settled(c.Ctx, build), "a store of another type replaces none")
	other := lowering.Settled(servicetest.Context(t, bindingsSpec, service.Policy{}).Ctx, build)
	assert.NotSame(t, first, other, "another context builds its own")
	assert.Equal(t, 2, builds, "one store of a type per run")
}

// TestPlace_IsTheOnlyPlaceASourceIndexIsSpelled pins the guarantee
// GitHub #86 exists for. Provenance built by hand is how a diagnostic shipped
// with none (#43) — and a source index written wrong misattributes a node just
// as silently, since nothing downstream can tell a wrong one from a right one.
func TestPlace_IsTheOnlyPlaceASourceIndexIsSpelled(t *testing.T) {
	t.Parallel()
	c := service.New(7, &load.Document{Doc: &soa.OpenAPI{}}, service.Policy{})

	assert.Equal(t, ir.Provenance{Source: 7, Pointer: "/components/schemas/User"},
		c.Place().At("/components/schemas/User"))
	assert.Equal(t, ir.Provenance{Source: 7}, c.Place().At(""),
		"a document-level position carries the index and no pointer")
}

// TestDiagAt_StampsAndHandsBack pins the split the Tier-1 conversion needs: the
// diagnostic arrives already located, and arrives as a value. A lowering with no
// accumulator can therefore still be sure of its provenance, which is what the
// converted leaves could not be while only the accumulating form existed.
func TestDiagAt_StampsAndHandsBack(t *testing.T) {
	t.Parallel()
	c := service.New(3, &load.Document{Doc: &soa.OpenAPI{}}, service.Policy{})

	got := c.DiagAt(ir.SeverityWarning, diag.DegradedConstruct, "/paths/~1x", "dropped %d of %d", 1, 2)

	assert.Equal(t, ir.SeverityWarning, got.Severity)
	assert.Equal(t, diag.DegradedConstruct, got.Code)
	assert.Equal(t, ir.Provenance{Source: 3, Pointer: "/paths/~1x"}, got.Provenance)
	assert.Equal(t, "dropped 1 of 2", got.Message, "the format arguments are applied")
}

// TestDynamicAnchor_ReadsTheBindingsOfItsDocument pins that the declarations
// are the table's for the document c reads, every one of them counted.
func TestDynamicAnchor_ReadsTheBindingsOfItsDocument(t *testing.T) {
	t.Parallel()
	src := openapitest.ComponentSpec("    A: {$dynamicAnchor: m, type: string}\n    B: {$dynamicAnchor: m}\n")
	doc, diags, err := load.Load(t.Context(), 0, compilers.Source{Path: "spec.yaml", Data: []byte(src)}, load.Options{})
	require.NoError(t, err)
	openapitest.RequireNoErrorDiags(t, diags)
	c := service.New(0, doc, service.Policy{})

	assert.Equal(t, bind.DynamicAnchor{Site: bind.Position{Pointer: "/components/schemas/A"}, Count: 2, Complete: true},
		c.DynamicAnchor("m"))
}

// TestPosition_PlacesAPointerInTheContextsDocument pins that a context reading
// the source places a pointer in the source.
func TestPosition_PlacesAPointerInTheContextsDocument(t *testing.T) {
	t.Parallel()
	c := servicetest.Context(t, bindingsSpec, service.Policy{})
	assert.Equal(t, bind.Position{Pointer: "/a"}, c.Position("/a"))
}

// TestNew_CarriesTheLimits pins that the budgets reach the walk through the
// context, beside the other policy the caller chose.
func TestNew_CarriesTheLimits(t *testing.T) {
	t.Parallel()
	limits := lowering.Limits{MaxEnumMembers: budget.AtMost(12)}

	c := servicetest.Context(t, bindingsSpec, service.Policy{Schema: lowering.Policy{Limits: limits}, Grouping: service.GroupByTags})

	assert.Equal(t, limits, c.Limits)
}

// TestNew_CarriesThePromotionPolicy pins that the promotion policy reaches the
// walk through the context, and that every copy of it reads one promoter: what
// a referent lends is read once for the run, whichever copy asks.
func TestNew_CarriesThePromotionPolicy(t *testing.T) {
	t.Parallel()
	policy := promotion.ExtensionPromotions{Targets: map[promotion.ExtensionTarget][]promotion.ExtensionSource{
		promotion.TargetRedaction: {{Key: "x-k"}},
	}}
	c := servicetest.Context(t, bindingsSpec, service.Policy{Schema: lowering.Policy{Promotions: policy}})
	assert.Equal(t, []string{"x-k"}, slices.Collect(c.Promoter().Keys()))

	reads := 0
	entries := func() ir.Unmodeled {
		reads++
		return ir.Unmodeled{"openapi:x-k": {Reason: ir.ReasonVendorExtension, Value: ir.RawValue(`true`)}}
	}
	at := bind.Position{Pointer: "/components/schemas/S"}
	assert.True(t, c.Promoter().Referent(at, false, entries).Redacted)
	assert.True(t, atSource(t, c.Ctx).Promoter().Referent(at, false, entries).Redacted)
	assert.Equal(t, 1, reads, "a copy of the context reads the promoter the context does")

	off := servicetest.Context(t, bindingsSpec, service.Policy{Schema: lowering.Policy{Promotions: promotion.ExtensionPromotions{Disabled: true}}})
	assert.Empty(t, slices.Collect(off.Promoter().Keys()))
}

// atSource returns the copy of c InDocument takes to the source's coordinates.
func atSource(t *testing.T, c lowering.Ctx) lowering.Ctx {
	t.Helper()
	at, ok := lowering.InDocument(c, bind.Source)
	require.True(t, ok)
	return at
}

// TestNew_ReadsWhatTheLoadPhaseReadAsValues pins what New makes of a Loaded:
// the context reads the source's own content, placed, named and positioned as
// its Documents entry says; another listed document is reached by InDocument
// alone; and what the caller handed over is copied, so a later write to it
// changes no context.
func TestNew_ReadsWhatTheLoadPhaseReadAsValues(t *testing.T) {
	t.Parallel()
	sources := []ir.SourceInfo{{Path: "spec.yaml"}, {Path: "ext.yaml"}}
	documents := map[bind.DocID]lowering.Document{
		bind.Source: {Place: provenance.Doc{Source: 3}},
		1:           {Name: "/r/ext.yaml", Place: provenance.Doc{Source: 4}, IDs: ids.In("ext.yaml")},
	}
	c := lowering.New(lowering.Loaded{Declares: resolve.NewNames("User"), Documents: documents, Sources: sources},
		lowering.Policy{})

	assert.Equal(t, ir.Provenance{Source: 3, Pointer: "/a"}, c.Place().At("/a"))
	assert.Equal(t, bind.Position{Doc: bind.Source, Pointer: "/a"}, c.Position("/a"))
	assert.True(t, c.DeclaresSchema("User"))
	assert.False(t, c.RefScope().Foreign)
	there, ok := lowering.InDocument(c, 1)
	require.True(t, ok)
	assert.Equal(t, ir.Provenance{Source: 4, Pointer: "/a"}, there.Place().At("/a"))
	assert.Equal(t, ids.In("ext.yaml"), there.IDs())
	assert.True(t, there.RefScope().Foreign)
	assert.Equal(t, "/r/ext.yaml", there.RefScope().Holder)
	_, ok = lowering.InDocument(c, 2)
	assert.False(t, ok, "a document the compile did not read")

	sources[0], documents[1] = ir.SourceInfo{Path: "other.yaml"}, lowering.Document{}
	assert.Equal(t, "spec.yaml", c.Sources()[0].Path, "the context holds a copy of the sources")
	there, ok = lowering.InDocument(c, 1)
	require.True(t, ok, "and of the documents")
	assert.Equal(t, 4, there.Place().Source)
}
