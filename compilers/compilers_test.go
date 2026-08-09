package compilers_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/ir"
)

// stubCompiler registers under fixed formats and returns an empty document. It
// recognizes a source whose bytes contain its marker, so a registry holding two
// of them can be asked which one claims a given source.
type stubCompiler struct {
	formats []compilers.SourceFormat
	marker  string
	detects compilers.SourceFormat
}

func (s *stubCompiler) Formats() []compilers.SourceFormat { return s.formats }

func (s *stubCompiler) Detect(src compilers.Source) (compilers.SourceFormat, bool) {
	if s.marker == "" || !bytes.Contains(src.Data, []byte(s.marker)) {
		return compilers.SourceFormat{}, false
	}
	if s.detects != (compilers.SourceFormat{}) {
		return s.detects, true
	}
	return s.formats[0], true
}

func (s *stubCompiler) DecodeOptions(compilers.OptionSet) (any, error) { return nil, nil }

func (s *stubCompiler) Compile(_ context.Context, _ []compilers.Source, _ compilers.Options) (*ir.Document, []ir.Diagnostic, error) {
	return &ir.Document{IRVersion: "0.1.0"}, nil, nil
}

func TestRegistry_RegisterAndLookup(t *testing.T) {
	t.Parallel()
	reg := compilers.NewRegistry()
	oa := &stubCompiler{formats: []compilers.SourceFormat{
		{Name: "openapi", Version: "3.0"},
		{Name: "openapi", Version: "3.1"},
	}, marker: "openapi"}
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

func TestSourceFormat_String(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "openapi@3.1", compilers.SourceFormat{Name: "openapi", Version: "3.1"}.String())
}

// TestRegistry_DetectAsksEachCompilerInRegistrationOrder is the seam that keeps
// format knowledge out of the layers above: the registry asks, the compilers
// answer, and adding a format is a registration rather than an edit somewhere
// else. Registration order is the tie-break, so the same source resolves the
// same way on every run.
func TestRegistry_DetectAsksEachCompilerInRegistrationOrder(t *testing.T) {
	t.Parallel()
	first := &stubCompiler{
		formats: []compilers.SourceFormat{{Name: "alpha", Version: "1"}},
		marker:  "shared",
	}
	second := &stubCompiler{
		formats: []compilers.SourceFormat{{Name: "beta", Version: "2"}},
		marker:  "shared",
	}
	reg := compilers.NewRegistry()
	require.NoError(t, reg.Register(first))
	require.NoError(t, reg.Register(second))

	got, format, ok := reg.Detect(compilers.Source{Path: "s.txt", Data: []byte("shared bytes")})
	require.True(t, ok)
	assert.Same(t, compilers.Compiler(first), got, "the earlier registration wins")
	assert.Equal(t, compilers.SourceFormat{Name: "alpha", Version: "1"}, format)

	_, format, ok = reg.Detect(compilers.Source{Path: "s.txt", Data: []byte("nobody claims this")})
	assert.False(t, ok)
	assert.Equal(t, compilers.SourceFormat{}, format, "the zero format means unrecognized")
}

// TestRegistry_DetectReportsRecognizedButUnregistered separates the two ways a
// source can go uncompiled: nothing recognized it, or something did and named a
// format the registry does not carry. A caller that could not tell them apart
// would report a known spec dialect as an unreadable file.
func TestRegistry_DetectReportsRecognizedButUnregistered(t *testing.T) {
	t.Parallel()
	front := &stubCompiler{
		formats: []compilers.SourceFormat{{Name: "alpha", Version: "1"}},
		marker:  "alpha",
		detects: compilers.SourceFormat{Name: "alpha", Version: "9"},
	}
	reg := compilers.NewRegistry()
	require.NoError(t, reg.Register(front))

	got, format, ok := reg.Detect(compilers.Source{Path: "s.txt", Data: []byte("alpha")})
	assert.False(t, ok, "no compiler is registered for alpha@9")
	assert.Nil(t, got)
	assert.Equal(t, compilers.SourceFormat{Name: "alpha", Version: "9"}, format)
}

func TestRegistry_DetectEmptySource(t *testing.T) {
	t.Parallel()
	reg := compilers.NewRegistry()
	require.NoError(t, reg.Register(&stubCompiler{
		formats: []compilers.SourceFormat{{Name: "alpha", Version: "1"}},
		marker:  "alpha",
	}))

	_, _, ok := reg.Detect(compilers.Source{Path: "empty.txt"})
	assert.False(t, ok, "no bytes declare no format")
}

// TestRegistry_DetectSkipsCompilersThatDecline pins that a compiler declining a
// source does not end the search.
func TestRegistry_DetectSkipsCompilersThatDecline(t *testing.T) {
	t.Parallel()
	declines := &stubCompiler{
		formats: []compilers.SourceFormat{{Name: "alpha", Version: "1"}},
		marker:  "alpha",
	}
	claims := &stubCompiler{
		formats: []compilers.SourceFormat{{Name: "beta", Version: "2"}},
		marker:  "beta",
	}
	reg := compilers.NewRegistry()
	require.NoError(t, reg.Register(declines))
	require.NoError(t, reg.Register(claims))

	got, format, ok := reg.Detect(compilers.Source{Path: "s.txt", Data: []byte("beta")})
	require.True(t, ok)
	assert.Same(t, compilers.Compiler(claims), got)
	assert.Equal(t, compilers.SourceFormat{Name: "beta", Version: "2"}, format)
}
