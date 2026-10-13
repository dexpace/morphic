package main

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSettingFlag_SetCollectsPairs(t *testing.T) {
	t.Parallel()
	got := settingFlag{}
	require.NoError(t, got.Set("grouping=path-prefix"))
	require.NoError(t, got.Set("overlay=patch.yaml"))
	// A value may carry the separator; only the first one splits the pair.
	require.NoError(t, got.Set("note=a=b"))
	// An empty value is a value, not an absent setting: the compiler decides
	// whether it can use one.
	require.NoError(t, got.Set("quiet="))

	assert.Equal(t, settingFlag{
		"grouping": "path-prefix",
		"overlay":  "patch.yaml",
		"note":     "a=b",
		"quiet":    "",
	}, got)
	assert.Equal(t, "grouping=path-prefix note=a=b overlay=patch.yaml quiet=", got.String())
}

func TestSettingFlag_StringOfNilRendersEmpty(t *testing.T) {
	t.Parallel()
	// pflag renders a zero value to decide whether to print a
	// default, so this is reached before any --opt is typed.
	var unset settingFlag
	assert.Empty(t, unset.String())
}

func TestSettingFlag_SetRefusals(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, raw, wantErr string
	}{
		{"no separator", "grouping", `want key=value, got "grouping"`},
		{"empty name", "=tags", `empty option name in "=tags"`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := settingFlag{}.Set(tc.raw)
			require.Error(t, err)
			assert.EqualError(t, err, tc.wantErr)
		})
	}

	repeated := settingFlag{}
	require.NoError(t, repeated.Set("grouping=tags"))
	err := repeated.Set("grouping=path-prefix")
	require.Error(t, err, "one of the two values would win with nothing to say which")
	assert.EqualError(t, err, `option "grouping" set more than once`)
	assert.Equal(t, "tags", repeated["grouping"], "the refused value must not land")

	var unset settingFlag
	assert.Error(t, unset.Set("grouping=tags"), "a nil set must refuse rather than panic")
}

// TestFlagValues_TypeNames pins the names help documents a flag's value by.
// pflag asks for one where the standard library's flag package did not, and an
// empty name would render as a bare flag with no hint of what it takes.
func TestFlagValues_TypeNames(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "key=value", settingFlag{}.Type())
	assert.Equal(t, "pointer", (&pointerFlag{}).Type())
	assert.Equal(t, "name", (&passFlag{}).Type())
}

// TestPassFlag_SetKeepsNamesAsTyped pins that the flag judges no name: a
// repeat and an empty one both reach the engine, which owns what they mean.
func TestPassFlag_SetKeepsNamesAsTyped(t *testing.T) {
	t.Parallel()

	var got passFlag
	assert.Empty(t, got.String())
	for _, name := range []string{"validate", "validate", ""} {
		require.NoError(t, got.Set(name))
	}

	assert.Empty(t, cmp.Diff(passFlag{"validate", "validate", ""}, got))
	assert.Equal(t, "validate validate ", got.String())
}

// TestSpecOptions_DisabledPasses pins that --skip-validate adds validate to
// whatever --disable-pass named, and that names reach the engine as typed.
func TestSpecOptions_DisabledPasses(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		opts specOptions
		want []string
	}{
		{"neither flag", specOptions{}, nil},
		{"--skip-validate", specOptions{skipValidate: true}, []string{"validate"}},
		{"--disable-pass validate", specOptions{disable: passFlag{"validate"}}, []string{"validate"}},
		{"both", specOptions{skipValidate: true, disable: passFlag{"validate"}},
			[]string{"validate", "validate"}},
		{"another name with --skip-validate", specOptions{skipValidate: true, disable: passFlag{"Nope"}},
			[]string{"Nope", "validate"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Empty(t, cmp.Diff(tt.want, tt.opts.disabledPasses()))
		})
	}
}
