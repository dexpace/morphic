package openapi

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"
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
	assert.Equal(t, rootSrcIndex, diags[0].Provenance.Source,
		"no document comes back, but SourceTable names the source a refusal indexes")
}

// TestParse_KeyThatIsNotUTF8IsRefused pins what ids.Ptr relies on: a source with
// a mapping key that is not UTF-8 is refused before anything lowers, so no such
// key becomes a pointer token.
func TestParse_KeyThatIsNotUTF8IsRefused(t *testing.T) {
	t.Parallel()
	spec := "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths: {}\n\xff: x\n"
	doc, diags, err := New().Compile(context.Background(),
		[]compilers.Source{openapitest.SourceOf(spec)}, compilers.Options{})
	require.NoError(t, err)
	assert.Nil(t, doc, "an undecodable source refuses to lower; no types are interned")
	assert.True(t, openapitest.HasDiag(diags, diag.UndecodableSource))
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
	types := compile.NewTypes()
	types.Register("", nil)
	require.Len(t, types.Violations(), 1, "the refusal is recorded before run reports it")

	_, diags, err := run(t.Context(), lowering.Ctx{Doc: &soa.OpenAPI{}}, types)
	require.NoError(t, err)

	assertHasErrorCode(t, diags, diag.InternalInvariant)
}

// TestWithoutRereported is a unit table over the ways a lowering diagnostic
// and the load phase's diagnostics can relate. Only one drops anything: the key
// withoutRereported dedupes on is the code and the whole Provenance, so another
// code at the same provenance survives, and so does the same code at another
// pointer, or at the same pointer in another source.
func TestWithoutRereported(t *testing.T) {
	t.Parallel()
	at := ir.Provenance{Source: 0, Pointer: "/components/schemas/S"}
	elsewhere := ir.Provenance{Source: 0, Pointer: "/components/schemas/T"}
	inOverlay := ir.Provenance{Source: 1, Pointer: "/components/schemas/S"}

	tests := map[string]struct {
		lowered []ir.Diagnostic
		loaded  []ir.Diagnostic
		want    []ir.Diagnostic
	}{
		"a different code at the same provenance is kept": {
			lowered: []ir.Diagnostic{{Code: diag.DegradedConstruct, Provenance: at}},
			loaded:  []ir.Diagnostic{{Code: diag.UnresolvedRef, Provenance: at}},
			want:    []ir.Diagnostic{{Code: diag.DegradedConstruct, Provenance: at}},
		},
		"unresolved-ref at a different provenance is kept": {
			lowered: []ir.Diagnostic{{Code: diag.UnresolvedRef, Provenance: elsewhere}},
			loaded:  []ir.Diagnostic{{Code: diag.UnresolvedRef, Provenance: at}},
			want:    []ir.Diagnostic{{Code: diag.UnresolvedRef, Provenance: elsewhere}},
		},
		"unresolved-ref at the same pointer in another source is kept": {
			lowered: []ir.Diagnostic{{Code: diag.UnresolvedRef, Provenance: inOverlay}},
			loaded:  []ir.Diagnostic{{Code: diag.UnresolvedRef, Provenance: at}},
			want:    []ir.Diagnostic{{Code: diag.UnresolvedRef, Provenance: inOverlay}},
		},
		"unresolved-ref at the same provenance is dropped": {
			lowered: []ir.Diagnostic{{Code: diag.UnresolvedRef, Provenance: at}},
			loaded:  []ir.Diagnostic{{Code: diag.UnresolvedRef, Provenance: at}},
			want:    []ir.Diagnostic{},
		},
		"unresolved-ref where the load phase refused a cycle is dropped": {
			lowered: []ir.Diagnostic{{Code: diag.UnresolvedRef, Provenance: at}},
			loaded:  []ir.Diagnostic{{Code: diag.CyclicRef, Provenance: at}},
			want:    []ir.Diagnostic{},
		},
		"an empty loaded list keeps everything": {
			lowered: []ir.Diagnostic{
				{Code: diag.UnresolvedRef, Provenance: at},
				{Code: diag.DegradedConstruct, Provenance: elsewhere},
			},
			loaded: nil,
			want: []ir.Diagnostic{
				{Code: diag.UnresolvedRef, Provenance: at},
				{Code: diag.DegradedConstruct, Provenance: elsewhere},
			},
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := withoutRereported(tc.lowered, tc.loaded)
			if d := cmp.Diff(tc.want, got); d != "" {
				t.Errorf("withoutRereported(-want +got):\n%s", d)
			}
		})
	}
}
