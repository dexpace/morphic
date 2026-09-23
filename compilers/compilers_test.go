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
	// parsed stands in for what this compiler made of a source while
	// recognizing it, which the registry carries to its own Compile and to no
	// other compiler's.
	parsed any
	// saw records the options each Detect call was asked under.
	saw []compilers.Options
}

func (s *stubCompiler) Formats() []compilers.SourceFormat { return s.formats }

func (s *stubCompiler) Detect(src compilers.Source, opts compilers.Options) (compilers.Recognition, []ir.Diagnostic, bool) {
	s.saw = append(s.saw, opts)
	if s.marker == "" || !bytes.Contains(src.Data, []byte(s.marker)) {
		return compilers.Recognition{}, s.declines, false
	}
	if s.detects != (compilers.SourceFormat{}) {
		return compilers.Recognition{Format: s.detects, Parsed: s.parsed}, nil, true
	}
	return compilers.Recognition{Format: s.formats[0], Parsed: s.parsed}, nil, true
}

func (s *stubCompiler) DecodeOptions(compilers.OptionSet) (any, error) { return nil, nil }

func (s *stubCompiler) Compile(_ context.Context, _ []compilers.Source, _ compilers.Options) (*ir.Document, []ir.Diagnostic, error) {
	return &ir.Document{IRVersion: ir.IRVersion}, nil, nil
}

// detect runs reg.Detect with every compiler at its defaults and returns the
// parts most tests read.
func detect(t *testing.T, reg *compilers.Registry, src compilers.Source) (compilers.Compiler, compilers.Recognition, []ir.Diagnostic, bool) {
	t.Helper()
	det, ok, err := reg.Detect(t.Context(), src, nil)
	require.NoError(t, err, "no compiler here can fail to be configured")
	return det.Compiler, det.Recognition, det.Declined, ok
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

	got, rec, _, ok := detect(t, reg, compilers.Source{Path: "s.txt", Data: []byte("shared bytes")})
	require.True(t, ok)
	assert.Same(t, compilers.Compiler(first), got, "the earlier registration wins")
	assert.Equal(t, compilers.SourceFormat{Name: "alpha", Version: "1"}, rec.Format)

	_, rec, _, ok = detect(t, reg, compilers.Source{Path: "s.txt", Data: []byte("nobody claims this")})
	assert.False(t, ok)
	assert.Equal(t, compilers.SourceFormat{}, rec.Format, "the zero format means unrecognized")
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

	got, rec, _, ok := detect(t, reg, compilers.Source{Path: "s.txt", Data: []byte("alpha")})
	assert.False(t, ok, "no compiler is registered for alpha@9")
	assert.Nil(t, got)
	assert.Equal(t, compilers.SourceFormat{Name: "alpha", Version: "9"}, rec.Format)
}

func TestRegistry_DetectEmptySource(t *testing.T) {
	t.Parallel()
	reg := compilers.NewRegistry()
	require.NoError(t, reg.Register(&stubCompiler{
		formats: []compilers.SourceFormat{{Name: "alpha", Version: "1"}},
		marker:  "alpha",
	}))

	_, _, _, ok := detect(t, reg, compilers.Source{Path: "empty.txt"})
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

	got, rec, _, ok := detect(t, reg, compilers.Source{Path: "s.txt", Data: []byte("beta")})
	require.True(t, ok)
	assert.Same(t, compilers.Compiler(claims), got)
	assert.Equal(t, compilers.SourceFormat{Name: "beta", Version: "2"}, rec.Format)
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

	_, _, diags, ok := detect(t, reg, compilers.Source{Path: "s.txt", Data: []byte("not for you")})

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

	_, rec, diags, ok := detect(t, reg, compilers.Source{Path: "s.txt", Data: []byte("beta")})

	require.True(t, ok)
	assert.Equal(t, compilers.SourceFormat{Name: "beta", Version: "2"}, rec.Format)
	assert.Empty(t, diags, "the source found a compiler, so nothing declined is worth saying")
}

// claimsNothing recognizes every source and names no format, which the contract
// does not allow. It exists to pin what the registry does with a compiler that
// breaks it.
type claimsNothing struct{ stubCompiler }

func (claimsNothing) Detect(compilers.Source, compilers.Options) (compilers.Recognition, []ir.Diagnostic, bool) {
	return compilers.Recognition{}, nil, true
}

// TestRegistry_DetectSkipsACompilerThatClaimsWithoutNaming pins that a compiler
// answering "mine" while naming no format does not end the search. Ending it
// there would make a source the next compiler would have taken come back
// unrecognized, with nothing in the output naming the compiler that swallowed it.
func TestRegistry_DetectSkipsACompilerThatClaimsWithoutNaming(t *testing.T) {
	t.Parallel()
	broken := &claimsNothing{stubCompiler{
		formats: []compilers.SourceFormat{{Name: "alpha", Version: "1"}},
	}}
	claims := &stubCompiler{
		formats: []compilers.SourceFormat{{Name: "beta", Version: "2"}},
		marker:  "beta",
	}
	reg := compilers.NewRegistry()
	require.NoError(t, reg.Register(broken))
	require.NoError(t, reg.Register(claims))

	got, rec, _, ok := detect(t, reg, compilers.Source{Path: "s.txt", Data: []byte("beta")})

	require.True(t, ok, "the compiler after the broken one must still be asked")
	assert.Same(t, compilers.Compiler(claims), got)
	assert.Equal(t, compilers.SourceFormat{Name: "beta", Version: "2"}, rec.Format)
}

// TestRegistry_FormatsAreSortedNotRegistrationOrder pins both halves of what
// Formats answers: every format some compiler serves, and in an order that does
// not depend on how the registry was built. Registration order is deliberately
// not it — this answers "what does this build accept" for a reader, and the same
// set rendered two ways reads as two sets.
func TestRegistry_FormatsAreSortedNotRegistrationOrder(t *testing.T) {
	t.Parallel()
	reg := compilers.NewRegistry()
	require.NoError(t, reg.Register(&stubCompiler{formats: []compilers.SourceFormat{
		{Name: "smithy", Version: "2.0"},
		{Name: "openapi", Version: "3.1"},
	}}))
	require.NoError(t, reg.Register(&stubCompiler{formats: []compilers.SourceFormat{
		{Name: "openapi", Version: "3.0"},
	}}))

	assert.Equal(t, []compilers.SourceFormat{
		{Name: "openapi", Version: "3.0"},
		{Name: "openapi", Version: "3.1"},
		{Name: "smithy", Version: "2.0"},
	}, reg.Formats())
}

// TestRegistry_FormatsOfAnEmptyRegistryIsEmpty pins that the zero registry
// answers rather than panicking on its nil map.
func TestRegistry_FormatsOfAnEmptyRegistryIsEmpty(t *testing.T) {
	t.Parallel()
	assert.Empty(t, compilers.NewRegistry().Formats())
	assert.Empty(t, new(compilers.Registry).Formats())
}

// TestRegistry_DetectCarriesTheParseToItsOwnCompiler pins what makes a parse
// safe to hand on: it reaches the compiler that made it, and no other. A
// compiler may recognize a format another serves — an OpenAPI compiler names
// Swagger so the caller hears "unsupported" rather than "unreadable" — and the
// value it parsed is of a type only it can read, so carrying it across would
// hand one compiler another's internals.
func TestRegistry_DetectCarriesTheParseToItsOwnCompiler(t *testing.T) {
	t.Parallel()
	parse := struct{ tree string }{"alpha's own"}
	alpha := &stubCompiler{
		formats: []compilers.SourceFormat{{Name: "alpha", Version: "1"}},
		marker:  "alpha",
		parsed:  parse,
	}
	reg := compilers.NewRegistry()
	require.NoError(t, reg.Register(alpha))

	_, rec, _, ok := detect(t, reg, compilers.Source{Path: "s.txt", Data: []byte("alpha")})
	require.True(t, ok)
	assert.Equal(t, parse, rec.Parsed, "the compiler that parsed it is the one registered for the format")
}

// TestRegistry_DetectDropsAParseForAnotherCompiler is the other half. beta owns
// the format alpha recognized, so alpha's parse must not reach it; the format
// is still reported, because naming a format one does not serve is the whole
// point of recognizing it.
func TestRegistry_DetectDropsAParseForAnotherCompiler(t *testing.T) {
	t.Parallel()
	betaFormat := compilers.SourceFormat{Name: "beta", Version: "2"}
	alpha := &stubCompiler{
		formats: []compilers.SourceFormat{{Name: "alpha", Version: "1"}},
		marker:  "shared",
		detects: betaFormat,
		parsed:  struct{ tree string }{"alpha's own"},
	}
	beta := &stubCompiler{formats: []compilers.SourceFormat{betaFormat}, marker: "shared"}
	reg := compilers.NewRegistry()
	require.NoError(t, reg.Register(alpha))
	require.NoError(t, reg.Register(beta))

	got, rec, _, ok := detect(t, reg, compilers.Source{Path: "s.txt", Data: []byte("shared")})
	require.True(t, ok)
	assert.Same(t, beta, got, "the format's owner is what compiles it")
	assert.Equal(t, betaFormat, rec.Format)
	assert.Nil(t, rec.Parsed, "one compiler's parse is not another's to read")
}

// TestRegistry_DetectDropsAParseForAnUnregisteredFormat pins the same rule
// where there is no owner at all: a recognized format nobody serves reaches the
// caller as unsupported, and the recognizer's parse goes nowhere, since nothing
// will be asked to compile it.
func TestRegistry_DetectDropsAParseForAnUnregisteredFormat(t *testing.T) {
	t.Parallel()
	alpha := &stubCompiler{
		formats: []compilers.SourceFormat{{Name: "alpha", Version: "1"}},
		marker:  "alpha",
		detects: compilers.SourceFormat{Name: "alpha", Version: "9"},
		parsed:  struct{ tree string }{"alpha's own"},
	}
	reg := compilers.NewRegistry()
	require.NoError(t, reg.Register(alpha))

	_, rec, _, ok := detect(t, reg, compilers.Source{Path: "s.txt", Data: []byte("alpha")})
	require.False(t, ok, "recognized, and no compiler serves it")
	assert.Equal(t, compilers.SourceFormat{Name: "alpha", Version: "9"}, rec.Format)
	assert.Nil(t, rec.Parsed)
}
