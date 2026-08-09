package compilers_test

import (
	"context"
	"testing"

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
