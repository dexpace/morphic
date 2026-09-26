package openapi_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/ir"
)

// sourceTableSources is the one argument every case below hands SourceTable, so
// each is asked about the same compile compileWith actually ran.
var sourceTableSources = []compilers.Source{{Path: "spec.yaml", Data: []byte(overlaySpec)}}

// TestCompiler_SourceTableAgreesWithDocumentSourcesOnSuccess pins the contract
// half that matters most for a caller reading a successful compile:
// SourceTable is not just a fallback for a refusal, it is the same table
// Document.Sources ends up holding, so a caller cannot observe compiling twice
// and getting two different answers for what a Provenance.Source indexes.
func TestCompiler_SourceTableAgreesWithDocumentSourcesOnSuccess(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		opts openapi.Options
	}{
		{"no overlay", openapi.Options{}},
		{"an overlay that applies", openapi.Options{
			Overlay: &openapi.Overlay{Path: "patch.yaml", Data: []byte(addTagProperty)},
		}},
		{"a lax overlay that matches nothing", openapi.Options{
			Overlay: &openapi.Overlay{Path: "patch.yaml", Data: []byte(noMatch), Lax: true},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			doc, _ := compileWith(t, tt.opts)

			table := openapi.New().SourceTable(sourceTableSources, compilers.Options{FormatOptions: tt.opts})

			require.Len(t, table, len(doc.Sources), "table: %+v, doc.Sources: %+v", table, doc.Sources)
			for i := range table {
				assert.Equal(t, doc.Sources[i].Path, table[i].Path, "entry %d", i)
			}
		})
	}
}

// TestCompiler_SourceTableOnARefusal pins the reason SourceTable exists: a
// compile that refuses returns no Document at all, so it is the only table a
// caller has to resolve the refusal's own diagnostics against.
func TestCompiler_SourceTableOnARefusal(t *testing.T) {
	t.Parallel()

	t.Run("cyclic spec", func(t *testing.T) {
		t.Parallel()
		const cyclic = `openapi: 3.1.0
info: {title: t, version: '1'}
paths: {}
components: {schemas: {A: {$ref: '#/components/schemas/A'}}}
`
		sources := []compilers.Source{{Path: "spec.yaml", Data: []byte(cyclic)}}
		opts := compilers.Options{}

		doc, diags, err := openapi.New().Compile(t.Context(), sources, opts)
		require.NoError(t, err, "a cyclic spec is a document problem, not a Go error")
		require.Nil(t, doc, "the cycle refuses the compile")

		table := openapi.New().SourceTable(sources, opts)
		assert.Equal(t, []ir.SourceInfo{{Path: "spec.yaml"}}, table)
		assertSourcesResolve(t, table, diags)
	})

	t.Run("an overlay with a recursive anchor", func(t *testing.T) {
		t.Parallel()
		// The same shape internal/load/overlay_refusals_internal_test.go pins at
		// the loader's level (TestLoad_AnOverlayAnchorThatContainsItselfIsRefused):
		// an anchor inside the update aliases one of its own ancestors, which
		// gives the overlay library's clone no base case.
		const anchorCycle = "overlay: 1.0.0\ninfo: {title: o, version: \"1\"}\n" +
			"actions:\n  - target: $.components.schemas.Pet.properties\n    update: {p: &a [*a]}\n"
		sources := sourceTableSources
		opts := compilers.Options{FormatOptions: openapi.Options{
			Overlay: &openapi.Overlay{Path: "patch.yaml", Data: []byte(anchorCycle)},
		}}

		doc, diags, err := openapi.New().Compile(t.Context(), sources, opts)
		require.NoError(t, err, "a cyclic overlay is a document problem, not a Go error")
		require.Nil(t, doc, "the overlay's own cycle refuses the compile")

		table := openapi.New().SourceTable(sources, opts)
		assert.Equal(t, []ir.SourceInfo{{Path: "spec.yaml"}, {Path: "patch.yaml"}}, table)
		assertSourcesResolve(t, table, diags)

		found, _ := ir.FirstError(diags)
		assert.Equal(t, diag.CyclicRef, found.Code, "the anchor cycle is what refused it: %+v", diags)
	})
}

// assertSourcesResolve holds every diagnostic in diags to the invariant this PR
// establishes: whatever a refusal reports, a caller can resolve it against
// table without an index that names no entry and is not NoSource either.
func assertSourcesResolve(t *testing.T, table []ir.SourceInfo, diags []ir.Diagnostic) {
	t.Helper()
	require.NotEmpty(t, diags, "a refusal reports at least one finding")
	for _, d := range diags {
		if d.Provenance.Source == ir.NoSource {
			continue
		}
		assert.True(t, d.Provenance.Source >= 0 && d.Provenance.Source < len(table),
			"diagnostic %+v must index table %+v or carry ir.NoSource", d, table)
	}
}

// TestCompiler_SourceTableDirectly exercises SourceTable's own edge cases,
// apart from whether they would also compile.
func TestCompiler_SourceTableDirectly(t *testing.T) {
	t.Parallel()
	c := openapi.New()

	t.Run("FormatOptions of the wrong type names the sources alone", func(t *testing.T) {
		t.Parallel()
		table := c.SourceTable([]compilers.Source{{Path: "spec.yaml"}},
			compilers.Options{FormatOptions: "not an openapi.Options"})
		assert.Equal(t, []ir.SourceInfo{{Path: "spec.yaml"}}, table)
	})

	t.Run("a nil overlay adds nothing", func(t *testing.T) {
		t.Parallel()
		table := c.SourceTable([]compilers.Source{{Path: "spec.yaml"}},
			compilers.Options{FormatOptions: openapi.Options{Overlay: nil}})
		assert.Equal(t, []ir.SourceInfo{{Path: "spec.yaml"}}, table)
	})

	t.Run("multiple sources are listed in order", func(t *testing.T) {
		t.Parallel()
		table := c.SourceTable(
			[]compilers.Source{{Path: "a.yaml"}, {Path: "b.yaml"}, {Path: "c.yaml"}},
			compilers.Options{})
		assert.Equal(t, []ir.SourceInfo{{Path: "a.yaml"}, {Path: "b.yaml"}, {Path: "c.yaml"}}, table)
	})
}
