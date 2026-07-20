package frontend_test

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/frontend"
	"github.com/dexpace/morphic/ir"
)

// stubFrontend registers under fixed formats and returns an empty document.
type stubFrontend struct{ formats []frontend.SourceFormat }

func (s *stubFrontend) Formats() []frontend.SourceFormat { return s.formats }

func (s *stubFrontend) Parse(_ context.Context, _ []frontend.Source, _ frontend.Options) (*ir.Document, []ir.Diagnostic, error) {
	return &ir.Document{IRVersion: "0.1.0"}, nil, nil
}

func TestRegistry_RegisterAndLookup(t *testing.T) {
	t.Parallel()
	reg := frontend.NewRegistry()
	oa := &stubFrontend{formats: []frontend.SourceFormat{
		{Name: "openapi", Version: "3.0"},
		{Name: "openapi", Version: "3.1"},
	}}
	require.NoError(t, reg.Register(oa))

	got, ok := reg.Lookup(frontend.SourceFormat{Name: "openapi", Version: "3.1"})
	require.True(t, ok)
	assert.Same(t, frontend.Frontend(oa), got)

	_, ok = reg.Lookup(frontend.SourceFormat{Name: "smithy", Version: "2.0"})
	assert.False(t, ok)
}

func TestRegistry_RejectsDuplicateFormat(t *testing.T) {
	t.Parallel()
	reg := frontend.NewRegistry()
	fmtA := &stubFrontend{formats: []frontend.SourceFormat{{Name: "openapi", Version: "3.1"}}}
	fmtB := &stubFrontend{formats: []frontend.SourceFormat{{Name: "openapi", Version: "3.1"}}}
	require.NoError(t, reg.Register(fmtA))
	err := reg.Register(fmtB)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "openapi@3.1")
}

func TestRegistry_RejectsFrontendWithNoFormats(t *testing.T) {
	t.Parallel()
	reg := frontend.NewRegistry()
	err := reg.Register(&stubFrontend{formats: nil})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reports no formats")
}

func TestRegistry_Formats_SortedAndComplete(t *testing.T) {
	t.Parallel()
	reg := frontend.NewRegistry()
	require.NoError(t, reg.Register(&stubFrontend{formats: []frontend.SourceFormat{
		{Name: "swagger", Version: "2.0"},
		{Name: "openapi", Version: "3.1"},
		{Name: "openapi", Version: "3.0"},
	}}))

	got := reg.Formats()
	want := []frontend.SourceFormat{
		{Name: "openapi", Version: "3.0"},
		{Name: "openapi", Version: "3.1"},
		{Name: "swagger", Version: "2.0"},
	}
	assert.Empty(t, cmp.Diff(want, got))
}

func TestRegistry_Formats_Empty(t *testing.T) {
	t.Parallel()
	reg := frontend.NewRegistry()
	assert.Empty(t, reg.Formats())
}

func TestSourceFormat_String(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "openapi@3.1", frontend.SourceFormat{Name: "openapi", Version: "3.1"}.String())
}
