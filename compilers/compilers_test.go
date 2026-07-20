package compilers_test

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/ir"
)

// stubCompiler registers under fixed formats and returns an empty document.
type stubCompiler struct{ formats []compilers.SourceFormat }

func (s *stubCompiler) Formats() []compilers.SourceFormat { return s.formats }

func (s *stubCompiler) Compile(_ context.Context, _ []compilers.Source, _ compilers.Options) (*ir.Document, []ir.Diagnostic, error) {
	return &ir.Document{IRVersion: "0.1.0"}, nil, nil
}

func TestRegistry_RegisterAndLookup(t *testing.T) {
	t.Parallel()
	reg := compilers.NewRegistry()
	oa := &stubCompiler{formats: []compilers.SourceFormat{
		{Name: "openapi", Version: "3.0"},
		{Name: "openapi", Version: "3.1"},
	}}
	require.NoError(t, reg.Register(oa))

	got, ok := reg.Lookup(compilers.SourceFormat{Name: "openapi", Version: "3.1"})
	require.True(t, ok)
	assert.Same(t, compilers.Compiler(oa), got)

	_, ok = reg.Lookup(compilers.SourceFormat{Name: "smithy", Version: "2.0"})
	assert.False(t, ok)
}

func TestRegistry_RejectsDuplicateFormat(t *testing.T) {
	t.Parallel()
	reg := compilers.NewRegistry()
	fmtA := &stubCompiler{formats: []compilers.SourceFormat{{Name: "openapi", Version: "3.1"}}}
	fmtB := &stubCompiler{formats: []compilers.SourceFormat{{Name: "openapi", Version: "3.1"}}}
	require.NoError(t, reg.Register(fmtA))
	err := reg.Register(fmtB)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "openapi@3.1")
}

func TestRegistry_RejectsCompilerWithNoFormats(t *testing.T) {
	t.Parallel()
	reg := compilers.NewRegistry()
	err := reg.Register(&stubCompiler{formats: nil})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reports no formats")
}

func TestRegistry_Formats_SortedAndComplete(t *testing.T) {
	t.Parallel()
	reg := compilers.NewRegistry()
	require.NoError(t, reg.Register(&stubCompiler{formats: []compilers.SourceFormat{
		{Name: "swagger", Version: "2.0"},
		{Name: "openapi", Version: "3.1"},
		{Name: "openapi", Version: "3.0"},
	}}))

	got := reg.Formats()
	want := []compilers.SourceFormat{
		{Name: "openapi", Version: "3.0"},
		{Name: "openapi", Version: "3.1"},
		{Name: "swagger", Version: "2.0"},
	}
	assert.Empty(t, cmp.Diff(want, got))
}

func TestRegistry_Formats_Empty(t *testing.T) {
	t.Parallel()
	reg := compilers.NewRegistry()
	assert.Empty(t, reg.Formats())
}

func TestSourceFormat_String(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "openapi@3.1", compilers.SourceFormat{Name: "openapi", Version: "3.1"}.String())
}
