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

// flagValue is a flag's value as pflag sets and renders it.
type flagValue interface {
	Set(raw string) error
	String() string
	Type() string
}

// flagCase is one value a flag reads or refuses: refused is the text of the
// refusal, "" for a value the flag takes.
type flagCase struct {
	raw, refused string
}

// checkFlagValue sets a fresh value of the flag each of cases names, holding
// one it takes to rendering as typed, and one it refuses to its refusal and to
// leaving the flag as it was.
func checkFlagValue(t *testing.T, fresh func() flagValue, typeName string, cases []flagCase) {
	t.Helper()
	assert.Equal(t, typeName, fresh().Type())
	for _, tc := range cases {
		f := fresh()
		before := f.String()
		err := f.Set(tc.raw)
		if tc.refused == "" {
			require.NoError(t, err, "%q", tc.raw)
			assert.Equal(t, tc.raw, f.String())
			continue
		}
		require.Error(t, err, "%q", tc.raw)
		assert.Contains(t, err.Error(), tc.refused)
		assert.Equal(t, before, f.String(), "a refused %q leaves the flag as it was", tc.raw)
	}
}

// TestFormatFlag_SetReadsEachFormat pins the formats --diag-format takes, as
// literal text, and the refusal of any other, the empty string among them.
func TestFormatFlag_SetReadsEachFormat(t *testing.T) {
	t.Parallel()
	checkFlagValue(t, func() flagValue { return new(formatText) }, "format", []flagCase{
		{"text", ""}, {"json", ""},
		{"xml", `"xml" is no diagnostics format (known: text, json)`},
		{"", `"" is no diagnostics format`},
	})
}

// TestShowFlag_SetReadsEachSeverity pins the severities --show takes, and the
// refusal of what names none, a threshold's none and the empty string among
// them.
func TestShowFlag_SetReadsEachSeverity(t *testing.T) {
	t.Parallel()
	checkFlagValue(t, func() flagValue { return new(showFlag("info")) }, "severity", []flagCase{
		{"error", ""}, {"warning", ""}, {"info", ""},
		{"fatal", `"fatal" is no severity (known: error, warning, info)`},
		{"none", `"none" is no severity`},
		{"", `"" is no severity`},
	})
}

// TestLimitFlag_SetReadsACount pins the counts --diag-limit takes, zero
// among them, and the refusal of what is no count of 0 or more.
func TestLimitFlag_SetReadsACount(t *testing.T) {
	t.Parallel()
	checkFlagValue(t, func() flagValue { return new(limitFlag(3)) }, "count", []flagCase{
		{"0", ""}, {"1", ""}, {"250", ""},
		{"-1", `want a count of 0 or more, got "-1"`},
		{"all", `got "all"`},
		{"", `got ""`},
	})
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
