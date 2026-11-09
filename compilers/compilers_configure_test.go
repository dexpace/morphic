// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

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

// errUnconfigurable is what decoding a compiler's settings fails with in these
// tests.
var errUnconfigurable = errors.New("unknown setting")

// settings is a Configuration holding a setting, which each compiler asked
// decodes for itself.
func settings() compilers.Configuration {
	return compilers.Configuration{Settings: compilers.OptionSet{Settings: map[string]string{"k": "v"}}}
}

// undecodable is a stub compiler serving format, recognizing a source holding
// marker, whose DecodeOptions fails. It answers with a value anyway, as a
// decoder that got partway might, so a registry that used it rather than the
// defaults would be seen doing so.
func undecodable(format compilers.SourceFormat, marker string) *stubCompiler {
	return &stubCompiler{formats: []compilers.SourceFormat{format}, marker: marker, decoded: "half-decoded",
		decodeErr: errUnconfigurable}
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
// runs under the options the compiler would compile with, each decoding the
// settings for itself beside the one Reader, and that the ones the taker was
// configured with are the ones handed back for the compile.
func TestRegistry_DetectAsksEachCompilerUnderItsOwnOptions(t *testing.T) {
	t.Parallel()
	a := &stubCompiler{formats: []compilers.SourceFormat{alpha}, marker: "alpha", decoded: "for alpha"}
	b := &stubCompiler{formats: []compilers.SourceFormat{beta}, marker: "beta", decoded: "for beta"}
	reg := registryOf(t, a, b)
	cfg := settings()
	cfg.Reader = failingReader{}

	det, ok, err := reg.Detect(t.Context(), compilers.Source{Data: []byte("beta")}, cfg)

	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, []compilers.Options{{FormatOptions: "for alpha", Reader: failingReader{}}}, a.saw)
	assert.Equal(t, []compilers.Options{{FormatOptions: "for beta", Reader: failingReader{}}}, b.saw)
	assert.Equal(t, compilers.Options{FormatOptions: "for beta", Reader: failingReader{}}, det.Options)
}

// TestRegistry_DetectConfiguresADeclinerItCannotConfigureByDefaults pins that
// options one compiler cannot decode are not an error while another takes the
// source: they are that other compiler's, and the one they do not describe is
// asked under its defaults.
func TestRegistry_DetectConfiguresADeclinerItCannotConfigureByDefaults(t *testing.T) {
	t.Parallel()
	a := undecodable(alpha, "alpha")
	b := &stubCompiler{formats: []compilers.SourceFormat{beta}, marker: "beta", decoded: "for beta"}
	reg := registryOf(t, a, b)

	det, ok, err := reg.Detect(t.Context(), compilers.Source{Data: []byte("beta")}, settings())

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
	a := undecodable(alpha, "alpha")
	reg := registryOf(t, a)

	det, ok, err := reg.Detect(t.Context(), compilers.Source{Data: []byte("alpha")}, settings())

	require.ErrorIs(t, err, errUnconfigurable)
	assert.Contains(t, err.Error(), "compilers: options for alpha@1: decode: unknown setting",
		"the error names the format being compiled and the decoding that failed")
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
	recognizer := &stubCompiler{formats: []compilers.SourceFormat{alpha}, marker: "beta", detects: beta,
		parsed: "alpha's parse", decoded: "for alpha"}
	owner := &stubCompiler{formats: []compilers.SourceFormat{beta}, decoded: "for beta"}
	src := compilers.Source{Data: []byte("beta")}

	det, ok, err := registryOf(t, recognizer, owner).Detect(t.Context(), src, settings())
	require.NoError(t, err)
	require.True(t, ok)
	assert.Same(t, compilers.Compiler(owner), det.Compiler)
	assert.Equal(t, compilers.Options{FormatOptions: "for beta"}, det.Options)
	assert.Nil(t, det.Recognition.Parsed)

	_, ok, err = registryOf(t, recognizer, undecodable(beta, "")).Detect(t.Context(), src, settings())
	require.ErrorIs(t, err, errUnconfigurable)
	assert.False(t, ok)
}

// TestRegistry_DetectNamesAnUnservedFormatWithoutConfiguringIt pins that a
// format nothing registered serves is reported as recognized and unsupported,
// not as a configuration error: nothing is going to compile it.
func TestRegistry_DetectNamesAnUnservedFormatWithoutConfiguringIt(t *testing.T) {
	t.Parallel()
	gamma := compilers.SourceFormat{Name: "gamma", Version: "3"}
	recognizer := undecodable(alpha, "gamma")
	recognizer.detects = gamma
	reg := registryOf(t, recognizer)

	det, ok, err := reg.Detect(t.Context(), compilers.Source{Data: []byte("gamma")}, settings())

	require.NoError(t, err)
	assert.False(t, ok)
	assert.Equal(t, gamma, det.Recognition.Format)
	assert.Nil(t, det.Compiler)
}

// TestRegistry_DetectGivesDefaultsUnderTheZeroConfiguration pins the zero
// Configuration: no compiler decodes anything, and each is asked under its
// defaults.
func TestRegistry_DetectGivesDefaultsUnderTheZeroConfiguration(t *testing.T) {
	t.Parallel()
	a := undecodable(alpha, "alpha")
	reg := registryOf(t, a)

	det, ok, err := reg.Detect(t.Context(), compilers.Source{Data: []byte("alpha")}, compilers.Configuration{})

	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, compilers.Options{}, det.Options)
	assert.Equal(t, []compilers.Options{{}}, a.saw)
}

// TestRegistry_DetectHandsEachCompilerTheFormatOptionsWithoutSettings pins a
// Configuration holding no setting: each compiler is asked under FormatOptions
// as given, without decoding anything, and every one is handed its Reader.
func TestRegistry_DetectHandsEachCompilerTheFormatOptionsWithoutSettings(t *testing.T) {
	t.Parallel()
	a := undecodable(alpha, "alpha")
	b := undecodable(beta, "beta")
	docs := failingReader{}
	cfg := compilers.Configuration{FormatOptions: "typed", Reader: docs}

	det, ok, err := registryOf(t, a, b).Detect(t.Context(), compilers.Source{Data: []byte("beta")}, cfg)

	require.NoError(t, err)
	require.True(t, ok)
	want := compilers.Options{FormatOptions: "typed", Reader: docs}
	assert.Equal(t, want, det.Options)
	assert.Equal(t, []compilers.Options{want}, a.saw)
	assert.Equal(t, []compilers.Options{want}, b.saw)
}

// TestRegistry_RefusesAConfigurationSettingBothChannels pins that Detect and
// SourceLimit refuse a Configuration setting both FormatOptions and Settings,
// whatever the source, before asking any compiler anything.
func TestRegistry_RefusesAConfigurationSettingBothChannels(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		ask  func(*testing.T, *compilers.Registry, compilers.Configuration) error
	}{
		{"Detect", func(t *testing.T, r *compilers.Registry, cfg compilers.Configuration) error {
			det, ok, err := r.Detect(t.Context(), compilers.Source{Data: []byte("alpha")}, cfg)
			assert.False(t, ok)
			assert.Equal(t, compilers.Detection{}, det)
			return err
		}},
		{"Detect of an empty source", func(t *testing.T, r *compilers.Registry, cfg compilers.Configuration) error {
			det, ok, err := r.Detect(t.Context(), compilers.Source{Path: "empty.yaml"}, cfg)
			assert.False(t, ok)
			assert.Equal(t, compilers.Detection{}, det)
			return err
		}},
		{"SourceLimit", func(t *testing.T, r *compilers.Registry, cfg compilers.Configuration) error {
			limit, err := r.SourceLimit(cfg)
			assert.Zero(t, limit)
			return err
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			a := &stubCompiler{formats: []compilers.SourceFormat{alpha}, marker: "alpha", limit: 5, decoded: "for alpha"}
			cfg := settings()
			cfg.FormatOptions = "typed"

			err := tt.ask(t, registryOf(t, a), cfg)

			require.EqualError(t, err, "compilers: set FormatOptions or Settings, not both")
			assert.Empty(t, a.saw, "no compiler is asked to recognize the source")
			assert.Empty(t, a.limitsAsked, "nor for its limit")
		})
	}
}

// failingReader is a DocumentReader these tests hand on and never read
// through.
type failingReader struct{}

func (failingReader) ReadDocument(context.Context, compilers.DocumentRequest) ([]byte, error) {
	return nil, errors.New("not read in these tests")
}

// TestRegistry_DetectStopsOnACanceledContext pins that recognition, which may
// parse the source, is not started for a caller who has stopped asking.
func TestRegistry_DetectStopsOnACanceledContext(t *testing.T) {
	t.Parallel()
	a := &stubCompiler{formats: []compilers.SourceFormat{alpha}, marker: "alpha"}
	reg := registryOf(t, a)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, ok, err := reg.Detect(ctx, compilers.Source{Data: []byte("alpha")}, compilers.Configuration{})

	require.ErrorIs(t, err, context.Canceled)
	assert.False(t, ok)
	assert.Empty(t, a.saw, "no compiler is asked")
}
