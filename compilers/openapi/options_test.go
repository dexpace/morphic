package openapi

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
)

func TestParse_WrongFormatOptions(t *testing.T) {
	t.Parallel()
	_, _, err := New().Compile(context.Background(),
		[]compilers.Source{openapitest.SourceOf("openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths: {}\n")},
		compilers.Options{FormatOptions: "not-openapi-options"})
	require.Error(t, err, "wrong FormatOptions type is a programmer error")
}

func TestParse_ExplicitOptions(t *testing.T) {
	t.Parallel()
	spec := "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths:\n  /a/b:\n    get: {operationId: ab, responses: {\"200\": {description: ok}}}\n"
	doc, _, err := New().Compile(context.Background(), []compilers.Source{openapitest.SourceOf(spec)},
		compilers.Options{FormatOptions: Options{Grouping: GroupByPathPrefix}})
	require.NoError(t, err)
	require.NotNil(t, doc)
	assert.Equal(t, "a", doc.Services[0].Groups[0].Name.Source)
}
