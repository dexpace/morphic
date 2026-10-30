// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package oas_test

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/compilers/openapi/internal/oas"
)

// TestParseMinor_ReadsTheMajorMinorPrefix pins which version strings the table
// covers.
func TestParseMinor_ReadsTheMajorMinorPrefix(t *testing.T) {
	tests := []struct {
		version string
		want    oas.Minor
		ok      bool
	}{
		{"3.0.3", oas.V30, true},
		{"3.0", oas.V30, true},
		{"3.1.0", oas.V31, true},
		{"3.2.0-rc1", oas.V32, true},
		{"3.3.0", 0, false},
		{"2.0", 0, false},
		{"3", 0, false},
		{"", 0, false},
		{"3..1", 0, false},
		{".1", 0, false},
	}
	for _, tc := range tests {
		got, ok := oas.ParseMinor(tc.version)
		assert.Equal(t, tc.ok, ok, tc.version)
		assert.Equal(t, tc.want, got, tc.version)
	}
}

// TestParseMinor_AllocatesNothing pins what the lowering's dialect check costs:
// it asks this of every schema whose null it reads, so the prefix it compares
// is a slice of the version rather than a string built from its parts.
func TestParseMinor_AllocatesNothing(t *testing.T) {
	assert.Zero(t, testing.AllocsPerRun(100, func() { _, _ = oas.ParseMinor("3.1.0") }))
}

// TestMinor_StringSpellsTheMinor pins the spelling the messages use.
func TestMinor_StringSpellsTheMinor(t *testing.T) {
	assert.Equal(t, "3.0", oas.V30.String())
	assert.Equal(t, "3.1", oas.V31.String())
	assert.Equal(t, "3.2", oas.V32.String())
	assert.Equal(t, "none", oas.Minor(0).String())
}

// validSpan reports whether s is empty or runs forward over the minors the
// table covers, which every span a row writes must be.
func validSpan(s oas.Span) bool {
	if s.Since == 0 {
		return s.Until == 0
	}
	last := slices.Max(minors())
	return s.Since <= last && s.Until <= last && (s.Until == 0 || s.Until >= s.Since)
}

// overlaps reports whether the two spans share a minor.
func overlaps(a, b oas.Span) bool {
	return slices.ContainsFunc(minors(), func(m oas.Minor) bool { return a.Holds(m) && b.Holds(m) })
}

// TestSpan_HoldsTheMinorsItNames walks every constructor over every minor.
func TestSpan_HoldsTheMinorsItNames(t *testing.T) {
	tests := []struct {
		name string
		span oas.Span
		want [3]bool // V30, V31, V32
	}{
		{"always", oas.Always(), [3]bool{true, true, true}},
		{"from 3.1", oas.From(oas.V31), [3]bool{false, true, true}},
		{"only 3.1", oas.Only(oas.V31), [3]bool{false, true, false}},
		{"through 3.1", oas.Through(oas.V31), [3]bool{true, true, false}},
		{"zero", oas.Span{}, [3]bool{false, false, false}},
	}
	for _, tc := range tests {
		for i, m := range []oas.Minor{oas.V30, oas.V31, oas.V32} {
			assert.Equal(t, tc.want[i], tc.span.Holds(m), "%s at %s", tc.name, m)
		}
		assert.True(t, validSpan(tc.span), tc.name)
	}
}

// TestSpan_Later pins the question the engine asks of a span.
func TestSpan_Later(t *testing.T) {
	assert.True(t, oas.From(oas.V32).Later(oas.V31), "3.2 is after a 3.1 document")
	assert.False(t, oas.From(oas.V31).Later(oas.V31))
	assert.False(t, oas.Span{}.Later(oas.V30), "an empty span is never after anything")
}

// TestValidSpan_RefusesBackwardsAndUnknownRuns pins what the table tests take a
// well-formed span to be, so the check holding every row to it can fail.
func TestValidSpan_RefusesBackwardsAndUnknownRuns(t *testing.T) {
	assert.False(t, validSpan(oas.Span{Since: oas.V32, Until: oas.V30}), "runs backwards")
	assert.False(t, validSpan(oas.Span{Since: oas.V30, Until: 9}), "ends at an unknown minor")
	assert.False(t, validSpan(oas.Span{Since: 9}), "starts at an unknown minor")
	assert.False(t, validSpan(oas.Span{Until: oas.V31}), "an end with no start")
}

// TestOverlaps_OnSharedMinors pins when the table tests take two spans to
// conflict.
func TestOverlaps_OnSharedMinors(t *testing.T) {
	assert.True(t, overlaps(oas.Through(oas.V31), oas.From(oas.V31)))
	assert.False(t, overlaps(oas.Only(oas.V30), oas.From(oas.V31)))
	assert.False(t, overlaps(oas.Span{}, oas.Always()))
}
