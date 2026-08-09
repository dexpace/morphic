package lowering_test

import (
	"reflect"
	"testing"

	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/speakeasy-api/openapi/sequencedmap"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/overlay"
	"github.com/dexpace/morphic/ir"
)

// docDeclaring builds a document declaring the named component schemas, with no
// parser and no fixture — which is the point of deriving the index from the
// document rather than from a lowering pass.
func docDeclaring(names ...string) *soa.OpenAPI {
	elems := make([]*sequencedmap.Element[string, *oas3.JSONSchema[oas3.Referenceable]], 0, len(names))
	for _, n := range names {
		elems = append(elems, sequencedmap.NewElem(n,
			oas3.NewJSONSchemaFromSchema[oas3.Referenceable](&oas3.Schema{})))
	}
	return &soa.OpenAPI{Components: &soa.Components{Schemas: sequencedmap.New(elems...)}}
}

// TestCtx_HasNoExportedMap is the guard that makes "immutable by value" true
// rather than conventional. A struct copy shares a map rather than copying it,
// so an exported map field would be the one part of the context a callee could
// write to — and the write would be visible to its caller's caller, which is
// exactly the class of bug passing by value is meant to remove.
//
// Slices are held to the same rule for the same reason: a copy shares the
// backing array. The exported pointer to the document is deliberately not
// covered — it is shared by design and lowering never writes through it, which
// TestNew_KeepsTheDocumentItWasGiven pins.
func TestCtx_HasNoExportedMap(t *testing.T) {
	t.Parallel()
	rt := reflect.TypeOf(lowering.Ctx{})
	require.Positive(t, rt.NumField(), "a context with no fields would pass this vacuously")

	var checked int
	for i := range rt.NumField() {
		f := rt.Field(i)
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
			name: "every declared component", doc: docDeclaring("User", "Order", "A~B"),
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
			name: "the empty name is a name like any other", doc: docDeclaring(""),
			declares: []string{""}, denies: []string{"User"},
		},
		{
			name: "no document at all", doc: nil, denies: []string{"User", ""},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := lowering.New(0, tc.doc, ir.SourceInfo{}, "", lowering.Limits{}, overlay.Origin{})
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
	doc := docDeclaring("User")
	src := ir.SourceInfo{Format: "openapi@3.1", Path: "spec.yaml", Hash: "abc"}

	c := lowering.New(7, doc, src, lowering.GroupByPathPrefix, lowering.Limits{}, overlay.Origin{})

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
	doc := docDeclaring("User")
	src := ir.SourceInfo{Format: "openapi@3.1", Path: "spec.yaml", Hash: "abc"}
	before := lowering.New(7, doc, src, lowering.GroupByPathPrefix, lowering.Limits{}, overlay.Origin{})
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
			c := lowering.New(0, &soa.OpenAPI{OpenAPI: tc.version}, ir.SourceInfo{}, "", lowering.Limits{}, overlay.Origin{})
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
	c := lowering.New(0, docDeclaring("User"), ir.SourceInfo{Path: "spec.yaml"}, "", lowering.Limits{}, overlay.Origin{})

	scope := c.RefScope()

	assert.Equal(t, "spec.yaml", scope.SelfPath)
	require.NotNil(t, scope.Declares, "a scope with no predicate would resolve nothing")
	assert.True(t, scope.Declares("User"))
	assert.False(t, scope.Declares("Missing"))
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

	c := lowering.New(0, docDeclaring(), src, "", lowering.Limits{}, origin)

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

	c := lowering.New(0, docDeclaring(), src, "", lowering.Limits{}, overlay.Origin{})

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

	c := lowering.New(0, docDeclaring(), ir.SourceInfo{}, "", lowering.Limits{}, origin)

	assert.Equal(t, ir.Provenance{Source: 1, Pointer: "/info/description"},
		c.ProvenanceAt("/info/description"), "the overlay introduced this position")
	assert.Equal(t, ir.Provenance{Source: 0, Pointer: "/info/title"},
		c.ProvenanceAt("/info/title"), "and left this one alone")
	assert.Equal(t, ir.Provenance{Source: 1, Pointer: "/info/description"},
		c.DiagAt(ir.SeverityWarning, "x", "/info/description", "m").Provenance,
		"a diagnostic is stamped through the same question")
}
