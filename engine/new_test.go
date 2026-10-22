package engine_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/engine"
	"github.com/dexpace/morphic/internal/testspec"
	"github.com/dexpace/morphic/pass"
)

// passNames returns the names of passes, in order.
func passNames(passes []pass.Pass) []string {
	names := make([]string, 0, len(passes))
	for _, p := range passes {
		names = append(names, p.Name())
	}
	return names
}

// TestNew_DefaultsToTheBuiltInCompilersAndPasses pins what New builds with no
// option: every format Compilers serves, and the passes Passes lists, which an
// unknown name's refusal spells out in order.
func TestNew_DefaultsToTheBuiltInCompilersAndPasses(t *testing.T) {
	t.Parallel()
	reg := compilers.NewRegistry()
	for _, c := range engine.Compilers() {
		require.NoError(t, reg.Register(c))
	}

	eng, err := engine.New()
	require.NoError(t, err)
	_, refused := eng.Run(t.Context(), "/no/such/spec.yaml",
		engine.RunOptions{DisablePasses: []string{"no-such-pass"}})

	assert.Equal(t, reg.Formats(), eng.Formats())
	require.ErrorIs(t, refused, engine.ErrUnknownPass)
	assert.Contains(t, refused.Error(), "(known: "+strings.Join(passNames(engine.Passes()), ", ")+")")
}

// TestNew_LaterOptionWins pins that options apply in order: a second
// WithCompilers or WithPasses replaces what the first set, rather than adding
// to it.
func TestNew_LaterOptionWins(t *testing.T) {
	t.Parallel()
	eng, err := engine.New(
		engine.WithCompilers(errCompiler{}), engine.WithPasses(notePass("first")),
		engine.WithCompilers(splitDiagCompiler{}), engine.WithPasses(notePass("second")),
	)
	require.NoError(t, err)

	res, err := eng.Run(t.Context(), writeSpec(t, testspec.Tiny), engine.RunOptions{})

	require.NoError(t, err, "the failing compiler was replaced")
	assert.Equal(t, []string{"test/second"}, codesOf(res))
}

// TestNew_OptionsCopyTheirLists pins that an option keeps the list it was
// given, so changing the caller's slice after building it changes no engine.
func TestNew_OptionsCopyTheirLists(t *testing.T) {
	t.Parallel()
	cs := []compilers.Compiler{splitDiagCompiler{}}
	ps := []pass.Pass{notePass("kept")}
	withCompilers, withPasses := engine.WithCompilers(cs...), engine.WithPasses(ps...)
	cs[0], ps[0] = errCompiler{}, notePass("swapped")

	eng, err := engine.New(withCompilers, withPasses)
	require.NoError(t, err)
	res, err := eng.Run(t.Context(), writeSpec(t, testspec.Tiny), engine.RunOptions{})

	require.NoError(t, err)
	assert.Equal(t, []string{"test/kept"}, codesOf(res))
}

// TestNew_WithNoPassesRunsNone pins that an empty WithPasses is a list of no
// passes, unlike no WithPasses at all, which runs the default ones: the same
// dangling reference is reported only by the second engine.
func TestNew_WithNoPassesRunsNone(t *testing.T) {
	t.Parallel()
	bare, err := engine.New(engine.WithCompilers(danglingCompiler{}), engine.WithPasses())
	require.NoError(t, err)
	defaulted, err := engine.New(engine.WithCompilers(danglingCompiler{}))
	require.NoError(t, err)
	path := writeSpec(t, testspec.Tiny)

	none, err := bare.Run(t.Context(), path, engine.RunOptions{})
	require.NoError(t, err)
	validated, err := defaulted.Run(t.Context(), path, engine.RunOptions{})
	require.NoError(t, err)

	assert.Empty(t, codesOf(none))
	assert.Contains(t, codesOf(validated), "ir/dangling-type-ref")
}

// TestNew_RefusesWhatARunCouldNotAddress pins every construction New refuses
// beyond the compilers': a nil option, and a pass list a run could not call or
// turn a pass off in by name. Each is a Go error naming the position, and no
// engine comes back.
func TestNew_RefusesWhatARunCouldNotAddress(t *testing.T) {
	t.Parallel()
	stub := engine.WithCompilers(splitDiagCompiler{})
	tests := []struct {
		name string
		opts []engine.Option
		want string
	}{
		{"a nil option", []engine.Option{nil}, "engine: option 0 is nil"},
		{"a nil option after another", []engine.Option{stub, nil}, "engine: option 1 is nil"},
		{"a nil pass", []engine.Option{stub, engine.WithPasses(notePass("a"), nil)}, "engine: pass 1 is nil"},
		{"a typed nil pass", []engine.Option{stub, engine.WithPasses((*notePass)(nil))}, "engine: pass 0 is nil"},
		{"an unnamed pass", []engine.Option{stub, engine.WithPasses(notePass("a"), notePass(""))},
			"engine: pass 1 has no name, which a run turns it off by"},
		{"two passes of one name", []engine.Option{stub, engine.WithPasses(notePass("a"), notePass("b"), notePass("a"))},
			`engine: passes 0 and 2 are both named "a"; turning one off would turn off both`},
		{"a default pass twice", []engine.Option{engine.WithPasses(append(engine.Passes(), pass.Validator{})...)},
			`engine: passes 0 and 1 are both named "validate"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			eng, err := engine.New(tt.opts...)

			require.Error(t, err)
			assert.Nil(t, eng, "nothing usable comes back from a refused construction")
			assert.Contains(t, err.Error(), tt.want)
		})
	}
}

// codesOf returns the codes of res's diagnostics, in order.
func codesOf(res *engine.Result) []string {
	codes := make([]string, 0, len(res.Diagnostics))
	for _, d := range res.Diagnostics {
		codes = append(codes, d.Code)
	}
	return codes
}
