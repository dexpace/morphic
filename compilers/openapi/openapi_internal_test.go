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
	"github.com/dexpace/morphic/compilers/openapi/internal/load"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
)

func TestParse_UnsupportedVersion(t *testing.T) {
	t.Parallel()
	spec := "openapi: 2.0.0\ninfo: {title: T, version: \"1\"}\npaths: {}\n"
	doc, diags, err := New().Compile(context.Background(), []compilers.Source{sourceOf(spec)}, compilers.Options{})
	require.NoError(t, err)
	assert.Nil(t, doc, "unsupported version refuses to lower")
	assert.True(t, hasDiag(diags, diag.UnsupportedVersion))
}

func TestParse_UnmarshalError(t *testing.T) {
	t.Parallel()
	_, _, err := New().Compile(context.Background(),
		[]compilers.Source{sourceOf("\t\t: : : not valid : yaml\n\x00")}, compilers.Options{})
	require.Error(t, err)
}

// ghostRefsSpec references non-existent components everywhere so every
// resolve-or-skip path (unresolved GetObject → nil) and resolution-error branch
// is exercised without a panic.
const ghostRefsSpec = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a:
    parameters:
      - {$ref: '#/components/parameters/GhostParam'}
    get:
      operationId: getA
      callbacks:
        good:
          '{$url}': {$ref: '#/components/pathItems/GhostInner'}
        bad: {$ref: '#/components/callbacks/GhostCb'}
      requestBody: {$ref: '#/components/requestBodies/GhostBody'}
      responses:
        "200": {$ref: '#/components/responses/GhostResp'}
        "201":
          description: ok
          headers:
            X-H: {$ref: '#/components/headers/GhostHeader'}
          content:
            application/json:
              schema: {type: string}
              examples:
                one: {$ref: '#/components/examples/GhostEx'}
  /ref: {$ref: '#/components/pathItems/GhostItem'}
webhooks:
  hook: {$ref: '#/components/pathItems/GhostHook'}
`

func TestGhostRefs_AllResolversDegradeGracefully(t *testing.T) {
	t.Parallel()
	// Uses the internal lowerer directly so resolution errors surface as
	// diagnostics without failing the parse; the point is no panic and coverage
	// of every resolve-or-skip branch.
	loadedDoc, diags, err := load.Load(t.Context(), 0, sourceOf(ghostRefsSpec), loadOptions(Options{}.withDefaults()))
	require.NoError(t, err)
	require.NotNil(t, loadedDoc)
	out, lowerDiags := run(loweringCtx(loadedDoc, Options{}.withDefaults()), compile.NewTypes(0))
	require.NotNil(t, out)
	assert.True(t, hasDiag(append(diags, lowerDiags...), diag.UnresolvedRef), "unresolved refs reported")
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

	_, diags := run(lowering.Ctx{Doc: &soa.OpenAPI{}}, types)

	assertHasErrorCode(t, diags, diag.InternalInvariant)
}
