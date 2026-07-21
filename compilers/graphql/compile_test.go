package graphql_test // external test package — exercises only the public API

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/graphql"
	"github.com/dexpace/morphic/ir"
)

func TestFormats_ReportsGraphQLSDL(t *testing.T) {
	t.Parallel()
	formats := graphql.New().Formats()
	require.Len(t, formats, 1)
	assert.Equal(t, "graphql", formats[0].Name)
	assert.Equal(t, "sdl", formats[0].Version)
}

func TestCompile_NoSourcesErrors(t *testing.T) {
	t.Parallel()
	_, _, err := graphql.New().Compile(t.Context(), nil, compilers.Options{})
	require.Error(t, err)
}

func TestCompile_WrongOptionsTypeErrors(t *testing.T) {
	t.Parallel()
	src := []compilers.Source{{Path: "s.graphql", Data: []byte("type Query { ok: Boolean }")}}
	_, _, err := graphql.New().Compile(t.Context(), src, compilers.Options{FormatOptions: 42})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "graphql.Options")
}

func TestCompile_AcceptsTypedOptions(t *testing.T) {
	t.Parallel()
	src := []compilers.Source{{Path: "s.graphql", Data: []byte("type Query { ok: Boolean }")}}
	doc, _, err := graphql.New().Compile(t.Context(), src,
		compilers.Options{FormatOptions: graphql.Options{OmitDirectiveDefinitions: true}})
	require.NoError(t, err)
	require.NotNil(t, doc)
}

func TestCompile_SyntaxErrorIsDiagnostic(t *testing.T) {
	t.Parallel()
	src := []compilers.Source{{Path: "bad.graphql", Data: []byte("type Query {")}}
	doc, diags, err := graphql.New().Compile(t.Context(), src, compilers.Options{})
	require.NoError(t, err, "a syntax error is a spec problem, not a Go error")
	assert.Nil(t, doc, "the compiler refuses to lower an unparseable document")
	require.NotEmpty(t, diags)
	assert.Equal(t, ir.SeverityError, diags[0].Severity)
	assert.Equal(t, "graphql/parse", diags[0].Code)
}

func TestCompile_UnknownTypeIsWarning(t *testing.T) {
	t.Parallel()
	src := []compilers.Source{{Path: "s.graphql", Data: []byte("type Query { a: Missing }")}}
	doc, diags, err := graphql.New().Compile(t.Context(), src, compilers.Options{})
	require.NoError(t, err)
	require.NotNil(t, doc)
	var found bool
	for _, d := range diags {
		if d.Code == "graphql/unknown-type" {
			found = true
			assert.Equal(t, ir.SeverityWarning, d.Severity)
		}
	}
	assert.True(t, found, "a dangling type reference is diagnosed")
}

func TestCompile_MergesMultipleSources(t *testing.T) {
	t.Parallel()
	src := []compilers.Source{
		{Path: "root.graphql", Data: []byte("type Query { a: A }")},
		{Path: "a.graphql", Data: []byte("type A { id: ID! }")},
	}
	doc, diags, err := graphql.New().Compile(t.Context(), src, compilers.Options{})
	require.NoError(t, err)
	require.NotNil(t, doc)
	assertNoErrorDiags(t, diags)
	_, ok := doc.Types[namedID("A")].(*ir.Model)
	assert.True(t, ok, "a type defined in a second file resolves across the merge")
	require.Len(t, doc.Sources, 2)
	assert.Equal(t, 1, doc.Types[namedID("A")].Common().Provenance.Source, "A's provenance points at its own source file")
}

func TestCompile_DefaultRootTypesWithoutSchemaBlock(t *testing.T) {
	t.Parallel()
	src := []compilers.Source{{Path: "s.graphql", Data: []byte("type Query { ping: String }")}}
	doc, _, err := graphql.New().Compile(t.Context(), src, compilers.Options{})
	require.NoError(t, err)
	require.NotNil(t, doc)
	require.Len(t, doc.Services, 1)
	require.Len(t, doc.Services[0].Groups, 1, "Query is discovered without an explicit schema block")
	assert.Equal(t, "query", doc.Services[0].Groups[0].Name.Source)
}

func TestCompile_DuplicateTypeIsWarning(t *testing.T) {
	t.Parallel()
	sdl := "type A { x: Int } type A { y: Int } type Query { a: A }"
	doc, diags, err := graphql.New().Compile(t.Context(),
		[]compilers.Source{{Path: "s.graphql", Data: []byte(sdl)}}, compilers.Options{})
	require.NoError(t, err)
	require.NotNil(t, doc)
	var found bool
	for _, d := range diags {
		if d.Code == "graphql/duplicate-type" {
			found = true
			assert.Equal(t, ir.SeverityWarning, d.Severity)
		}
	}
	assert.True(t, found, "a redefined type name is diagnosed")
	a, ok := doc.Types[namedID("A")].(*ir.Model)
	require.True(t, ok)
	_, kept := propByWire(a, "x")
	assert.True(t, kept, "the first definition wins")
}

func TestCompile_ExtendMergesIntoBase(t *testing.T) {
	t.Parallel()
	sdl := `type A { x: Int }
	extend type A @tag(name: "t") { y: Int }
	type Query { a: A }`
	doc, diags, err := graphql.New().Compile(t.Context(),
		[]compilers.Source{{Path: "s.graphql", Data: []byte(sdl)}}, compilers.Options{})
	require.NoError(t, err)
	require.NotNil(t, doc)
	assertNoErrorDiags(t, diags)
	a, ok := doc.Types[namedID("A")].(*ir.Model)
	require.True(t, ok)
	_, hasX := propByWire(a, "x")
	_, hasY := propByWire(a, "y")
	assert.True(t, hasX && hasY, "extend fields merge into the base")
	assert.Contains(t, a.Extensions, "graphql:extends", "the extend occurrence is recorded on a based type")
	assert.Contains(t, a.Extensions, "federation:@tag", "an extend's directives merge in")
}

func TestCompile_FieldLevelFederationDetectsV1(t *testing.T) {
	t.Parallel()
	sdl := "type Widget { id: ID! @external } type Query { w: Widget }"
	doc, _, err := graphql.New().Compile(t.Context(),
		[]compilers.Source{{Path: "s.graphql", Data: []byte(sdl)}}, compilers.Options{})
	require.NoError(t, err)
	require.NotNil(t, doc)
	raw, ok := doc.Extensions["federation:version"]
	require.True(t, ok, "a field-level federation directive alone detects v1")
	assert.JSONEq(t, `"1"`, string(raw))
}

func TestCompile_UndefinedSchemaRootIsSkipped(t *testing.T) {
	t.Parallel()
	sdl := "type Query { a: Int } schema { query: Query, mutation: Ghost }"
	doc, _, err := graphql.New().Compile(t.Context(),
		[]compilers.Source{{Path: "s.graphql", Data: []byte(sdl)}}, compilers.Options{})
	require.NoError(t, err)
	require.NotNil(t, doc)
	require.Len(t, doc.Services, 1)
	assert.Len(t, doc.Services[0].Groups, 1, "a schema block root that names no defined type yields no group")
}

func TestCompile_DirectiveOnlyExtendYieldsFieldlessModel(t *testing.T) {
	t.Parallel()
	sdl := `extend type Widget @tag(name: "x")
	type Query { ok: Boolean }`
	doc, _, err := graphql.New().Compile(t.Context(),
		[]compilers.Source{{Path: "s.graphql", Data: []byte(sdl)}}, compilers.Options{})
	require.NoError(t, err)
	require.NotNil(t, doc)
	w, ok := doc.Types[namedID("Widget")].(*ir.Model)
	require.True(t, ok, "a directive-only extend still lands as a model")
	assert.Empty(t, w.Properties, "a fieldless extend produces a model with no properties")
}

func TestCompile_InaccessibleTypeIsInternal(t *testing.T) {
	t.Parallel()
	sdl := `type Hidden @inaccessible { x: Int }
	type Query { h: Hidden }`
	doc, _, err := graphql.New().Compile(t.Context(),
		[]compilers.Source{{Path: "s.graphql", Data: []byte(sdl)}}, compilers.Options{})
	require.NoError(t, err)
	require.NotNil(t, doc)
	hidden, ok := doc.Types[namedID("Hidden")].(*ir.Model)
	require.True(t, ok)
	assert.Equal(t, "internal", hidden.Common().Access, "@inaccessible on a type maps to internal access")
}
