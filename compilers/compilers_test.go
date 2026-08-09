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
	// declines is what this compiler says when it does not take a source, which
	// the registry is meant to carry out for the caller to report.
	declines []ir.Diagnostic
}

func (s *stubCompiler) Formats() []compilers.SourceFormat { return s.formats }

func (s *stubCompiler) Detect(src compilers.Source) (compilers.SourceFormat, []ir.Diagnostic, bool) {
	if s.marker == "" || !bytes.Contains(src.Data, []byte(s.marker)) {
		return compilers.SourceFormat{}, s.declines, false
	}
	if s.detects != (compilers.SourceFormat{}) {
		return s.detects, nil, true
	}
	return s.formats[0], nil, true
}

func (s *stubCompiler) DecodeOptions(compilers.OptionSet) (any, error) { return nil, nil }

func (s *stubCompiler) Compile(_ context.Context, _ []compilers.Source, _ compilers.Options) (*ir.Document, []ir.Diagnostic, error) {
	return &ir.Document{IRVersion: ir.IRVersion}, nil, nil
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

// TestRegistry_RejectsNilCompiler covers both spellings of a nil compiler. Each
// case asserts an error rather than a panic: Register's only use of its argument
// is the c.Formats() call, so before the guard existed both of these were a
// segmentation fault raised inside this package and delivered to the caller.
func TestRegistry_RejectsNilCompiler(t *testing.T) {
	t.Parallel()
	cases := map[string]compilers.Compiler{
		"untyped nil interface": nil,
		"typed nil pointer":     (*stubCompiler)(nil),
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			reg := compilers.NewRegistry()
			err := reg.Register(c)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "nil compiler")
		})
	}
}

// TestRegistry_ZeroValueRegisters pins the zero value as a usable registry. A
// Registry built as a literal has a nil map, and writing to one panics, so the
// alternative to allocating on first use is a second landmine beside the one
// this change removes.
func TestRegistry_ZeroValueRegisters(t *testing.T) {
	t.Parallel()
	var reg compilers.Registry
	oa := &stubCompiler{formats: []compilers.SourceFormat{{Name: "openapi", Version: "3.1"}}}

	_, ok := reg.Lookup(compilers.SourceFormat{Name: "openapi", Version: "3.1"})
	require.False(t, ok, "an unregistered format misses before anything is registered")
	require.NoError(t, reg.Register(oa))

	got, ok := reg.Lookup(compilers.SourceFormat{Name: "openapi", Version: "3.1"})
	require.True(t, ok)
	assert.Same(t, compilers.Compiler(oa), got)
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

	got, format, _, ok := reg.Detect(compilers.Source{Path: "s.txt", Data: []byte("shared bytes")})
	require.True(t, ok)
	assert.Same(t, compilers.Compiler(first), got, "the earlier registration wins")
	assert.Equal(t, compilers.SourceFormat{Name: "alpha", Version: "1"}, format)

	_, format, _, ok = reg.Detect(compilers.Source{Path: "s.txt", Data: []byte("nobody claims this")})
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

	got, format, _, ok := reg.Detect(compilers.Source{Path: "s.txt", Data: []byte("alpha")})
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

	_, _, _, ok := reg.Detect(compilers.Source{Path: "empty.txt"})
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

	got, format, _, ok := reg.Detect(compilers.Source{Path: "s.txt", Data: []byte("beta")})
	require.True(t, ok)
	assert.Same(t, compilers.Compiler(claims), got)
	assert.Equal(t, compilers.SourceFormat{Name: "beta", Version: "2"}, format)
}

// TestRegistry_DetectCarriesWhatDecliningCompilersSaid pins the channel the
// contract opened: a compiler that declines a source it recognizes as its own
// and broken has something to report, and the registry is what carries it out to
// a caller that would otherwise have only "nobody claimed these bytes".
func TestRegistry_DetectCarriesWhatDecliningCompilersSaid(t *testing.T) {
	t.Parallel()
	said := ir.NewDiagnostic(ir.SeverityError, "alpha/broken", "malformed alpha",
		ir.Provenance{Source: ir.NoSource})
	reg := compilers.NewRegistry()
	require.NoError(t, reg.Register(&stubCompiler{
		formats:  []compilers.SourceFormat{{Name: "alpha", Version: "1"}},
		marker:   "alpha",
		declines: []ir.Diagnostic{said},
	}))

	_, _, diags, ok := reg.Detect(compilers.Source{Path: "s.txt", Data: []byte("not for you")})

	require.False(t, ok)
	assert.Equal(t, []ir.Diagnostic{said}, diags)
}

// TestRegistry_DetectDropsDeclinesOnceClaimed pins the other half: a compiler
// that takes the source ends the search, so an earlier decliner's account of why
// it passed is moot. Carrying it anyway would attach a complaint to a compile
// that went on to succeed.
func TestRegistry_DetectDropsDeclinesOnceClaimed(t *testing.T) {
	t.Parallel()
	reg := compilers.NewRegistry()
	require.NoError(t, reg.Register(&stubCompiler{
		formats: []compilers.SourceFormat{{Name: "alpha", Version: "1"}},
		marker:  "alpha",
		declines: []ir.Diagnostic{ir.NewDiagnostic(ir.SeverityError, "alpha/broken", "malformed alpha",
			ir.Provenance{Source: ir.NoSource})},
	}))
	require.NoError(t, reg.Register(&stubCompiler{
		formats: []compilers.SourceFormat{{Name: "beta", Version: "2"}},
		marker:  "beta",
	}))

	_, format, diags, ok := reg.Detect(compilers.Source{Path: "s.txt", Data: []byte("beta")})

	require.True(t, ok)
	assert.Equal(t, compilers.SourceFormat{Name: "beta", Version: "2"}, format)
	assert.Empty(t, diags, "the source found a compiler, so nothing declined is worth saying")
}
