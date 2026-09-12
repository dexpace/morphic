package openapi

import (
	"context"
	"testing"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

func TestParse_UnsupportedVersion(t *testing.T) {
	t.Parallel()
	spec := "openapi: 2.0.0\ninfo: {title: T, version: \"1\"}\npaths: {}\n"
	doc, diags, err := New().Compile(context.Background(), []compilers.Source{openapitest.SourceOf(spec)}, compilers.Options{})
	require.NoError(t, err)
	assert.Nil(t, doc, "unsupported version refuses to lower")
	assert.True(t, openapitest.HasDiag(diags, diag.UnsupportedVersion))
}

// TestParse_UnmarshalError pins where a document that will not parse is
// reported. It is a finding about the source, not a failure of the compiler:
// engine.Run turns a compiler's Go error into one of its own, and the CLI reads
// that as having been invoked wrong rather than as a spec it could not read.
func TestParse_UnmarshalError(t *testing.T) {
	t.Parallel()
	doc, diags, err := New().Compile(context.Background(),
		[]compilers.Source{openapitest.SourceOf("\t\t: : : not valid : yaml\n\x00")}, compilers.Options{})
	require.NoError(t, err)
	assert.Nil(t, doc)
	require.Len(t, diags, 1)
	assert.Equal(t, diag.UndecodableSource, diags[0].Code)
	assert.Equal(t, ir.NoSource, diags[0].Provenance.Source,
		"no document comes back, so there is no source table for a Source of 0 to index into")
}

// TestRun_RegistryRefusalsAreSurfaced covers the reporting of an entry
// compile.Types declined to hold.
//
// No spec can provoke one — an empty ID or a nil type definition is a compiler
// bug — so the refusal is forced directly. Without this the framework would
// decline garbage and say nothing, which hides the bug instead of the symptom:
// the node is simply absent and every reference to it dangles.
func TestRun_RegistryRefusalsAreSurfaced(t *testing.T) {
	t.Parallel()
	types := compile.NewTypes(0)
	types.Register("", nil)
	require.Len(t, types.Violations(), 1, "the refusal is recorded before run reports it")

	_, diags, err := run(t.Context(), lowering.Ctx{Doc: &soa.OpenAPI{}}, types)
	require.NoError(t, err)

	assertHasErrorCode(t, diags, diag.InternalInvariant)
}
