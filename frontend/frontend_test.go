package frontend_test

import (
	"context"
	"testing"

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

func TestSourceFormat_String(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "openapi@3.1", frontend.SourceFormat{Name: "openapi", Version: "3.1"}.String())
}
