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
