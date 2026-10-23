package compilers_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
)

var (
	alpha = compilers.SourceFormat{Name: "alpha", Version: "1"}
	beta  = compilers.SourceFormat{Name: "beta", Version: "2"}
)

// errUnconfigurable is what configuring a compiler fails with in these tests.
var errUnconfigurable = errors.New("unknown setting")

// configureBy answers with a per-compiler FormatOptions value, and with
// errUnconfigurable for any compiler it holds no value for. A failed answer
// carries a value anyway, as a decoder that got partway might, so a registry
// that used it rather than the defaults would be seen doing so.
func configureBy(values map[compilers.Compiler]string) compilers.Configure {
	return func(c compilers.Compiler) (compilers.Options, error) {
		v, ok := values[c]
		if !ok {
			return compilers.Options{FormatOptions: "half-decoded"}, errUnconfigurable
		}
		return compilers.Options{FormatOptions: v}, nil
	}
}

func registryOf(t *testing.T, cs ...compilers.Compiler) *compilers.Registry {
	t.Helper()
	reg := compilers.NewRegistry()
	for _, c := range cs {
		require.NoError(t, reg.Register(c))
	}
	return reg
}

// TestRegistry_DetectAsksEachCompilerUnderItsOwnOptions pins that recognition
// runs under the options the compiler would compile with, and that the ones the
// taker was configured with are the ones handed back for the compile.
func TestRegistry_DetectAsksEachCompilerUnderItsOwnOptions(t *testing.T) {
	t.Parallel()
	a := &stubCompiler{formats: []compilers.SourceFormat{alpha}, marker: "alpha"}
	b := &stubCompiler{formats: []compilers.SourceFormat{beta}, marker: "beta"}
	reg := registryOf(t, a, b)

	det, ok, err := reg.Detect(t.Context(), compilers.Source{Data: []byte("beta")},
		configureBy(map[compilers.Compiler]string{a: "for alpha", b: "for beta"}))

	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, []compilers.Options{{FormatOptions: "for alpha"}}, a.saw)
	assert.Equal(t, []compilers.Options{{FormatOptions: "for beta"}}, b.saw)
	assert.Equal(t, compilers.Options{FormatOptions: "for beta"}, det.Options)
}

// TestRegistry_DetectConfiguresADeclinerItCannotConfigureByDefaults pins that
// options one compiler cannot decode are not an error while another takes the
// source: they are that other compiler's, and the one they do not describe is
// asked under its defaults.
func TestRegistry_DetectConfiguresADeclinerItCannotConfigureByDefaults(t *testing.T) {
	t.Parallel()
	a := &stubCompiler{formats: []compilers.SourceFormat{alpha}, marker: "alpha"}
	b := &stubCompiler{formats: []compilers.SourceFormat{beta}, marker: "beta"}
	reg := registryOf(t, a, b)

	det, ok, err := reg.Detect(t.Context(), compilers.Source{Data: []byte("beta")},
		configureBy(map[compilers.Compiler]string{b: "for beta"}))

	require.NoError(t, err)
	require.True(t, ok)
	assert.Same(t, compilers.Compiler(b), det.Compiler)
	assert.Equal(t, []compilers.Options{{}}, a.saw, "asked under its defaults")
}

// TestRegistry_DetectFailsWhenTheTakerCannotBeConfigured pins the other half:
// the compiler taking the source is the one the options were for, so failing to
// configure it is the caller's error — after it was asked under its defaults.
func TestRegistry_DetectFailsWhenTheTakerCannotBeConfigured(t *testing.T) {
	t.Parallel()
	a := &stubCompiler{formats: []compilers.SourceFormat{alpha}, marker: "alpha"}
	reg := registryOf(t, a)

	det, ok, err := reg.Detect(t.Context(), compilers.Source{Data: []byte("alpha")},
		configureBy(map[compilers.Compiler]string{}))

	require.ErrorIs(t, err, errUnconfigurable)
	assert.Contains(t, err.Error(), "alpha@1", "the error names the format being compiled")
	assert.False(t, ok)
	assert.Equal(t, compilers.Detection{}, det)
	assert.Equal(t, []compilers.Options{{}}, a.saw)
}

// TestRegistry_DetectConfiguresTheOwnerNotTheRecognizer pins that a compiler
// recognizing a format another serves hands over neither its parse nor its
// options: the owner is configured in its own right, and failing to configure
// it is the error even though the recognizer was configured fine.
func TestRegistry_DetectConfiguresTheOwnerNotTheRecognizer(t *testing.T) {
	t.Parallel()
	recognizer := &stubCompiler{formats: []compilers.SourceFormat{alpha}, marker: "beta", detects: beta, parsed: "alpha's parse"}
	owner := &stubCompiler{formats: []compilers.SourceFormat{beta}}
	reg := registryOf(t, recognizer, owner)
	src := compilers.Source{Data: []byte("beta")}

	det, ok, err := reg.Detect(t.Context(), src,
		configureBy(map[compilers.Compiler]string{recognizer: "for alpha", owner: "for beta"}))
	require.NoError(t, err)
	require.True(t, ok)
	assert.Same(t, compilers.Compiler(owner), det.Compiler)
	assert.Equal(t, compilers.Options{FormatOptions: "for beta"}, det.Options)
	assert.Nil(t, det.Recognition.Parsed)

	_, ok, err = reg.Detect(t.Context(), src,
		configureBy(map[compilers.Compiler]string{recognizer: "for alpha"}))
	require.ErrorIs(t, err, errUnconfigurable)
	assert.False(t, ok)
}

// TestRegistry_DetectNamesAnUnservedFormatWithoutConfiguringIt pins that a
// format nothing registered serves is reported as recognized and unsupported,
// not as a configuration error: nothing is going to compile it.
func TestRegistry_DetectNamesAnUnservedFormatWithoutConfiguringIt(t *testing.T) {
	t.Parallel()
	gamma := compilers.SourceFormat{Name: "gamma", Version: "3"}
	reg := registryOf(t, &stubCompiler{formats: []compilers.SourceFormat{alpha}, marker: "gamma", detects: gamma})

	det, ok, err := reg.Detect(t.Context(), compilers.Source{Data: []byte("gamma")},
		configureBy(map[compilers.Compiler]string{}))

	require.NoError(t, err)
	assert.False(t, ok)
	assert.Equal(t, gamma, det.Recognition.Format)
	assert.Nil(t, det.Compiler)
}

// TestRegistry_DetectGivesDefaultsWithoutAConfigure pins the nil Configure.
func TestRegistry_DetectGivesDefaultsWithoutAConfigure(t *testing.T) {
	t.Parallel()
	a := &stubCompiler{formats: []compilers.SourceFormat{alpha}, marker: "alpha"}
	reg := registryOf(t, a)

	det, ok, err := reg.Detect(t.Context(), compilers.Source{Data: []byte("alpha")}, nil)

	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, compilers.Options{}, det.Options)
	assert.Equal(t, []compilers.Options{{}}, a.saw)
}

// TestRegistry_DetectStopsOnACanceledContext pins that recognition, which may
// parse the source, is not started for a caller who has stopped asking.
func TestRegistry_DetectStopsOnACanceledContext(t *testing.T) {
	t.Parallel()
	a := &stubCompiler{formats: []compilers.SourceFormat{alpha}, marker: "alpha"}
	reg := registryOf(t, a)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, ok, err := reg.Detect(ctx, compilers.Source{Data: []byte("alpha")}, nil)

	require.ErrorIs(t, err, context.Canceled)
	assert.False(t, ok)
	assert.Empty(t, a.saw, "no compiler is asked")
}
