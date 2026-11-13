// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

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

// TestFindingsFlags_ReadWhatTheyTake pins each value --diag-format, --show and
// --diag-limit take, as literal text, and that each renders as typed.
func TestFindingsFlags_ReadWhatTheyTake(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{"text", "json"} {
		var f formatFlag
		require.NoError(t, f.Set(raw))
		assert.Equal(t, raw, f.String())
	}
	for _, raw := range []string{"error", "warning", "info"} {
		var f showFlag
		require.NoError(t, f.Set(raw))
		assert.Equal(t, raw, f.String())
	}
	for _, raw := range []string{"0", "1", "250"} {
		var f limitFlag
		require.NoError(t, f.Set(raw))
		assert.Equal(t, raw, f.String())
	}
	var (
		format formatFlag
		show   showFlag
		limit  limitFlag
	)
	assert.Equal(t, []string{"format", "severity", "count"}, []string{format.Type(), show.Type(), limit.Type()})
}

// TestFindingsFlags_RefuseWhatNamesNothing pins each refusal, the empty string
// among them, which names no format, severity or count, and that a refused
// value leaves the flag as it was.
func TestFindingsFlags_RefuseWhatNamesNothing(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		flag interface {
			Set(string) error
			String() string
		}
		raw, want string
	}{
		{"an unknown format", new(formatText), "xml", `"xml" is no diagnostics format (known: text, json)`},
		{"no format", new(formatText), "", `"" is no diagnostics format`},
		{"an unknown severity", new(showFlag("info")), "fatal", `"fatal" is no severity (known: error, warning, info)`},
		{"none, a threshold and no severity", new(showFlag("info")), "none", `"none" is no severity`},
		{"no severity", new(showFlag("info")), "", `"" is no severity`},
		{"a negative count", new(limitFlag(3)), "-1", `want a count of 0 or more, got "-1"`},
		{"no number", new(limitFlag(3)), "all", `got "all"`},
		{"no count", new(limitFlag(3)), "", `got ""`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			before := tc.flag.String()
			err := tc.flag.Set(tc.raw)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
			assert.Equal(t, before, tc.flag.String())
		})
	}
}

// TestRun_FindingsFlagsRefuseABadValue pins a bad value of each flag as a
// misuse, exit 2, before anything is read.
func TestRun_FindingsFlagsRefuseABadValue(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{{"--diag-format", "xml"}, {"--show", "fatal"}, {"--diag-limit", "-1"}} {
		t.Run(args[0], func(t *testing.T) {
			t.Parallel()
			code, stdout, stderr := runCommand(t, append([]string{"validate", specFile(t)}, args...)...)
			assert.Equal(t, exitFailure, code)
			assert.Empty(t, stdout)
			assert.Contains(t, stderr, `invalid argument "`+args[1]+`" for "`+args[0]+`"`)
		})
	}
}
