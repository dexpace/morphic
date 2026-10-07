package lowering_test

import (
	"encoding/json/jsontext"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/load"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/overlay"
	"github.com/dexpace/morphic/compilers/openapi/internal/resolve"
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
			c := lowering.New(0, tc.doc, ir.SourceInfo{}, "", lowering.Limits{}, lowering.StreamingMedia{}, lowering.ExtensionPromotions{}, overlay.Origin{})
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

	c := lowering.New(7, doc, src, lowering.GroupByPathPrefix, lowering.Limits{}, lowering.StreamingMedia{}, lowering.ExtensionPromotions{}, overlay.Origin{})

	assert.Same(t, doc, c.Doc, "the document is referenced, never copied")
	assert.Equal(t, src, c.Source)
	assert.Equal(t, lowering.GroupByPathPrefix, c.Grouping)
	assert.Equal(t, 7, c.SrcIndex)
}

// TestWithAuth_ExtendsACopy pins the half of the auth design that makes the
// "no window in which it reads empty" claim true: the extended context answers
// for the schemes it was given, and the context it was derived from still
// answers for none. A WithAuth that wrote through would make every lowering
// above the security-scheme phase able to see them.
func TestWithAuth_ExtendsACopy(t *testing.T) {
	t.Parallel()
	doc := openapitest.DocDeclaring("User")
	src := ir.SourceInfo{Format: "openapi@3.1", Path: "spec.yaml", Hash: "abc"}
	before := lowering.New(7, doc, src, lowering.GroupByPathPrefix, lowering.Limits{}, lowering.StreamingMedia{}, lowering.ExtensionPromotions{}, overlay.Origin{})
	schemes := map[ir.AuthID]ir.AuthScheme{"a/apiKey": {ID: "a/apiKey"}}

	after := before.WithAuth(schemes)

	assert.True(t, after.DeclaresAuth("a/apiKey"), "the extended context answers for what it was given")
	assert.False(t, after.DeclaresAuth("a/other"), "and only for that")
	assert.False(t, before.DeclaresAuth("a/apiKey"),
		"the context it was derived from is unchanged, so nothing before the auth phase can read them")

	// The schemes are the only difference. Asserting that is not pedantry: a
	// WithAuth that built a fresh context around the map would satisfy all three
	// assertions above and hand the service walk a context with no document in
	// it, and the declared-name index — unexported, so no field comparison would
	// notice — is what every $ref resolved below here reads.
	assert.Same(t, doc, after.Doc, "the document survives the extension")
	assert.Equal(t, src, after.Source)
	assert.Equal(t, 7, after.SrcIndex)
	assert.Equal(t, lowering.GroupByPathPrefix, after.Grouping)
	assert.True(t, after.DeclaresSchema("User"), "as does the index derived at entry")
}

// TestDeclaresAuth_TheZeroContextDeclaresNothing pins the nil-map read, which is
// the state every lowering before the security-scheme phase holds.
func TestDeclaresAuth_TheZeroContextDeclaresNothing(t *testing.T) {
	t.Parallel()
	var c lowering.Ctx
	assert.False(t, c.DeclaresAuth("a/apiKey"))
	assert.False(t, c.DeclaresAuth(""))
}

// TestExclusiveBoundIsBoolean_TheZeroContextReadsAsNumeric completes the set the
// two accessors above start: every reader on this type answers on a context with
// nothing in it. This is the one that reaches through the document pointer, so
// it is the one that would panic instead — and a test holding a Ctx literal is
// not a hypothetical, it is how half this file drives the type.
func TestExclusiveBoundIsBoolean_TheZeroContextReadsAsNumeric(t *testing.T) {
	t.Parallel()
	var c lowering.Ctx
	assert.False(t, c.ExclusiveBoundIsBoolean(),
		"no document names no dialect, which falls back to the 2020-12 numeric form")
}

// TestExclusiveBoundIsBoolean_FollowsTheDialect pins which spelling of
// exclusiveMinimum/exclusiveMaximum the document uses. Reading a 3.0 boolean as
// a 2020-12 numeric bound (or the reverse) silently changes what constraint the
// IR records, and neither form fails to parse as the other.
func TestExclusiveBoundIsBoolean_FollowsTheDialect(t *testing.T) {
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
			c := lowering.New(0, &soa.OpenAPI{OpenAPI: tc.version}, ir.SourceInfo{}, "", lowering.Limits{}, lowering.StreamingMedia{}, lowering.ExtensionPromotions{}, overlay.Origin{})
			assert.Equal(t, tc.want, c.ExclusiveBoundIsBoolean())
		})
	}
}

// TestRefScope_IsTheContextSeenAsAScope pins the two facts reference resolution
// reads, and that both come from the context rather than from a copy beside it:
// the document's own path decides internal from external, and the declared set
// decides whether an internal pointer names anything.
func TestRefScope_IsTheContextSeenAsAScope(t *testing.T) {
	t.Parallel()
	c := lowering.New(0, openapitest.DocDeclaring("User"), ir.SourceInfo{Path: "spec.yaml"}, "", lowering.Limits{}, lowering.StreamingMedia{}, lowering.ExtensionPromotions{}, overlay.Origin{})

	scope := c.RefScope()

	assert.Equal(t, "spec.yaml", scope.SelfPath)
	require.NotNil(t, scope.Declares, "a scope with no predicate would resolve nothing")
	assert.True(t, scope.Declares("User"))
	assert.False(t, scope.Declares("Missing"))
}

// TestRefScope_NoDocumentResolvesNoDefsPointer pins that a context with no
// document hands over a scope with none, rather than one holding a nil
// pointer: that passes every nil check a reader makes and then faults when a
// "#/$defs/..." pointer is navigated in it.
func TestRefScope_NoDocumentResolvesNoDefsPointer(t *testing.T) {
	t.Parallel()
	scope := lowering.Ctx{}.RefScope()

	_, ok := scope.TargetPointer(&oas3.JSONSchema[oas3.Referenceable]{}, "#/$defs/n")
	assert.False(t, ok, "no document to read a definition from")
}

// TestRefScope_SharesTheContextsReader pins that every scope a context hands
// out, from the context or any copy of it, reads "#/$defs/..." pointers through
// the one reader New built: its memory is what keeps a reference's cost from
// growing with how deep its schema sits, and a reader per scope would have none.
func TestRefScope_SharesTheContextsReader(t *testing.T) {
	t.Parallel()
	c := lowering.New(0, openapitest.DocDeclaring("A"), ir.SourceInfo{}, "", lowering.Limits{}, lowering.StreamingMedia{}, lowering.ExtensionPromotions{}, overlay.Origin{})

	require.NotNil(t, c.RefScope().Defs, "a context with a document reads through a reader")
	assert.Same(t, c.RefScope().Defs, c.RefScope().Defs)
	assert.Same(t, c.RefScope().Defs, c.NamingByReference().RefScope().Defs, "a copy shares it")
	assert.Nil(t, lowering.Ctx{}.RefScope().Defs, "a context with no document has none")
}

// TestRefScope_CarriesTheMappingTargetsItWasGiven pins that a mapping target
// the load phase resolved reaches the scope through the context. The target is
// an extension's value, a position the parsed model holds as raw YAML, so the
// scope finds it only if WithMappingTargets handed it over (GitHub #757).
func TestRefScope_CarriesTheMappingTargetsItWasGiven(t *testing.T) {
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
	require.NotNil(t, loaded.Targets.At("/x-lib/Cat"), "the load phase resolved the target")

	bare := lowering.New(0, loaded.Doc, loaded.Source, "", lowering.Limits{}, lowering.StreamingMedia{}, lowering.ExtensionPromotions{}, loaded.Overlay)
	assert.Nil(t, bare.RefScope().DeclaredAt("/x-lib/Cat"), "the model holds the target as raw YAML")

	carrying := bare.WithMappingTargets(loaded.Targets)
	assert.Same(t, loaded.Targets.At("/x-lib/Cat"), carrying.RefScope().DeclaredAt("/x-lib/Cat"))
	assert.Nil(t, bare.RefScope().DeclaredAt("/x-lib/Cat"), "the context it was derived from still has none")
}

// TestTypePosition_IsReadOncePerScope pins the answers every copy of a context
// shares: a position is read once in each scope it is read in, whatever its
// answer, and a copy reads what another remembered. A copy carrying other
// mapping targets starts afresh, since an answer reads them, and a zero
// context remembers nothing.
func TestTypePosition_IsReadOncePerScope(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "ext.yaml"),
		[]byte("paths:\n  /x: {get: {responses: {\"200\": {description: ok}}}}\n"), 0o600))
	const root = "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths:\n  /ext: {$ref: './ext.yaml#/paths/~1x'}\n"
	doc, _, err := load.Load(t.Context(), 0, compilers.Source{Path: filepath.Join(dir, "root.yaml"),
		Data: []byte(root)}, load.Options{AllowExternalRefs: true})
	require.NoError(t, err)
	c := lowering.New(0, doc.Doc, doc.Source, "", lowering.Limits{}, lowering.StreamingMedia{},
		lowering.ExtensionPromotions{}, overlay.Origin{})
	reads := 0
	ask := func(c lowering.Ctx, pointer jsontext.Pointer) {
		t.Helper()
		at, ok := c.TypePosition(pointer, func() (jsontext.Pointer, bool) {
			reads++
			return pointer + "/there", pointer != "/none"
		})
		assert.Equal(t, pointer+"/there", at)
		assert.Equal(t, pointer != "/none", ok)
	}

	ask(c, "/x")
	ask(c.NamingByReference(), "/x")
	assert.Equal(t, 1, reads, "a copy reads what the context remembered")
	ask(c, "/none")
	ask(c, "/none")
	assert.Equal(t, 2, reads, "a position naming none is remembered too")
	foreign := c.At("/paths/~1ext/get/responses/200")
	require.True(t, foreign.RefScope().Foreign)
	ask(foreign, "/x")
	assert.Equal(t, 3, reads, "the position is read again in another document's scope")
	ask(c.WithMappingTargets(load.MappingTargets{}), "/x")
	assert.Equal(t, 4, reads, "a copy carrying other targets remembers nothing of c's")
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
		lowering.ExtensionPromotions{}, overlay.Origin{})
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

	origin, diags := overlay.Apply(index, &root, overlay.Options{Path: "patch.yaml", Data: []byte(ov)})
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

	c := lowering.New(0, openapitest.DocDeclaring(), src, "", lowering.Limits{}, lowering.StreamingMedia{}, lowering.ExtensionPromotions{}, origin)

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

	c := lowering.New(0, openapitest.DocDeclaring(), src, "", lowering.Limits{}, lowering.StreamingMedia{}, lowering.ExtensionPromotions{}, overlay.Origin{})

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

	c := lowering.New(0, openapitest.DocDeclaring(), ir.SourceInfo{}, "", lowering.Limits{}, lowering.StreamingMedia{}, lowering.ExtensionPromotions{}, origin)

	assert.Equal(t, ir.Provenance{Source: 1, Pointer: "/info/description"},
		c.ProvenanceAt("/info/description"), "the overlay introduced this position")
	assert.Equal(t, ir.Provenance{Source: 0, Pointer: "/info/title"},
		c.ProvenanceAt("/info/title"), "and left this one alone")
	assert.Equal(t, ir.Provenance{Source: 1, Pointer: "/info/description"},
		c.DiagAt(ir.SeverityWarning, "x", "/info/description", "m").Provenance,
		"a diagnostic is stamped through the same question")
}

// TestCtx_NamingByReferenceIsScopedToTheCopy holds what makes the flag safe to
// thread: it is set on a copy, so a lowering that descends under a $ref cannot
// leak the marking back to the caller that is still lowering a declaration.
func TestCtx_NamingByReferenceIsScopedToTheCopy(t *testing.T) {
	t.Parallel()
	declaring := lowering.Ctx{}
	assert.False(t, declaring.NamesByReference(), "a context names by declaration by default")

	referencing := declaring.NamingByReference()
	assert.True(t, referencing.NamesByReference())
	assert.False(t, declaring.NamesByReference(),
		"the caller's context is unchanged, so the marking cannot escape the subtree")

	assert.True(t, referencing.NamingByReference().NamesByReference(),
		"and marking an already-marked context is a no-op rather than a toggle")
}

// TestCtx_NamingByReferenceAtMarksOnlyAForeignDeclaration pins the question the
// marking is derived from: whether the declaration a lowering is about to read
// sits at the position that reached it.
//
// The two pointers agree exactly when a construct is declared where it is used,
// and differ exactly when a $ref carried the lowering into a declaration another
// position owns. Deciding it here rather than at each call site is what keeps a
// request body and a response header answering it the same way (GitHub #433).
func TestCtx_NamingByReferenceAtMarksOnlyAForeignDeclaration(t *testing.T) {
	t.Parallel()
	const use = "/paths/~1b/post/requestBody"
	declaring := lowering.Ctx{}

	assert.False(t, declaring.NamingByReferenceAt(use, use).NamesByReference(),
		"a construct declared where it is used is reached through its own declaration")

	foreign := declaring.NamingByReferenceAt(use, "/paths/~1a/post/requestBody")
	assert.True(t, foreign.NamesByReference(),
		"a declaration another position owns is reached by reference, so its name is a placeholder")
	assert.False(t, declaring.NamesByReference(),
		"and the caller's context is left alone, as NamingByReference leaves it")

	assert.True(t, foreign.NamingByReferenceAt(use, use).NamesByReference(),
		"a marked context stays marked: the subtree under a $ref is named by reference throughout, "+
			"however its own positions line up")
}
