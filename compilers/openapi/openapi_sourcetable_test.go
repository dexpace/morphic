// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package openapi_test

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/engine/reader"
	"github.com/dexpace/morphic/ir"
)

// sourceTableSpec is the document the cases compile: one component schema,
// Pet, whose properties the overlays target, written last so a case can append
// a schema of its own.
var sourceTableSpec = openapitest.ComponentSpec("    Pet:\n      type: object\n      properties:\n        name: {type: string}\n")

// tagOverlay adds a property to Pet, so a compile under it applies an action.
const tagOverlay = "overlay: 1.0.0\ninfo: {title: Patch, version: \"1\"}\n" +
	"actions:\n  - target: $.components.schemas.Pet.properties\n    update:\n      tag: {type: string}\n"

// missOverlay targets a path the document does not declare, which only a lax
// overlay lets pass.
const missOverlay = "overlay: 1.0.0\ninfo: {title: Patch, version: \"1\"}\n" +
	"actions:\n  - target: $.paths['/nope']\n    update: {description: x}\n"

// sourceTableSources is sourceTableSpec as the one source a case hands to both
// Compile and SourceTable.
var sourceTableSources = []compilers.Source{{Path: "spec.yaml", Data: []byte(sourceTableSpec)}}

// TestCompiler_SourceTableBeginsDocumentSourcesOnSuccess pins the contract
// half that matters most for a caller reading a successful compile:
// SourceTable is not just a fallback for a refusal, it is the table
// Document.Sources begins with, in order, so a caller cannot observe compiling
// twice and getting two answers for what an index into it names. Past it
// come only the documents the compile read, here one a reference names.
func TestCompiler_SourceTableBeginsDocumentSourcesOnSuccess(t *testing.T) {
	t.Parallel()
	split := sourceTableSpec + "    Thing: {$ref: 'other.yaml#/Thing'}\n"
	other := compilers.Source{Path: "other.yaml", Data: []byte("Thing: {type: string}\n")}
	tests := []struct {
		name string
		spec string
		opts openapi.Options
		read []string
	}{
		{"no overlay", sourceTableSpec, openapi.Options{}, nil},
		{"an overlay that applies", sourceTableSpec, openapi.Options{
			Overlay: &openapi.Overlay{Path: "patch.yaml", Data: []byte(tagOverlay)},
		}, nil},
		{"a lax overlay that matches nothing", sourceTableSpec, openapi.Options{
			Overlay: &openapi.Overlay{Path: "patch.yaml", Data: []byte(missOverlay), Lax: true},
		}, nil},
		{"a reference into another document", split, openapi.Options{}, []string{"other.yaml"}},
		{"an overlay and a reference", split, openapi.Options{
			Overlay: &openapi.Overlay{Path: "patch.yaml", Data: []byte(tagOverlay)},
		}, []string{"other.yaml"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			sources := []compilers.Source{{Path: "spec.yaml", Data: []byte(tt.spec)}}
			docs, err := reader.NewMemory(append(slices.Clone(sources), other))
			require.NoError(t, err)
			opts := compilers.Options{FormatOptions: tt.opts, Reader: docs}
			doc, diags, err := openapi.New().Compile(t.Context(), sources, opts)
			require.NoError(t, err)
			require.NotNil(t, doc, "compile refused: %+v", diags)

			table := openapi.New().SourceTable(sources, opts)

			require.Len(t, doc.Sources, len(table)+len(tt.read), "table: %+v, doc.Sources: %+v", table, doc.Sources)
			for i, entry := range table {
				assert.Equal(t, entry.Path, doc.Sources[i].Path, "entry %d", i)
			}
			for i, path := range tt.read {
				assert.Equal(t, path, doc.Sources[len(table)+i].Path, "read %d", i)
			}
		})
	}
}

// TestCompiler_SourceTableOnARefusal pins the reason SourceTable exists: a
// compile that refuses returns no Document at all, so it is the only table a
// caller has to resolve the refusal's own diagnostics against.
func TestCompiler_SourceTableOnARefusal(t *testing.T) {
	t.Parallel()

	t.Run("a source with a recursive anchor", func(t *testing.T) {
		t.Parallel()
		const cyclic = `openapi: 3.1.0
info: {title: t, version: '1'}
paths: {}
x-a: &a [*a]
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
		// an alias inside the update names one of its own ancestors, which gives
		// the overlay library's clone no base case.
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

// assertSourcesResolve holds every diagnostic in diags to the Compile contract:
// whatever a refusal reports, a caller can resolve it against table without an
// index that names no entry and is not NoSource either.
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
