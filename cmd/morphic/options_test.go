package main

import (
	"testing"

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
	// The flag package renders a zero value to decide whether to print a
	// default, so this is reached before any -opt is typed.
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
