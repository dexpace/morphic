// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package specrules_test

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/compilers/openapi/internal/specrules"
)

// TestParseMinor_ReadsTheMajorMinorPrefix pins which version strings the table
// covers.
func TestParseMinor_ReadsTheMajorMinorPrefix(t *testing.T) {
	tests := []struct {
		version string
		want    specrules.Minor
		ok      bool
	}{
		{"3.0.3", specrules.V30, true},
		{"3.0", specrules.V30, true},
		{"3.1.0", specrules.V31, true},
		{"3.2.0-rc1", specrules.V32, true},
		{"3.3.0", 0, false},
		{"2.0", 0, false},
		{"3", 0, false},
		{"", 0, false},
		{"3..1", 0, false},
		{".1", 0, false},
	}
	for _, tc := range tests {
		got, ok := specrules.ParseMinor(tc.version)
		assert.Equal(t, tc.ok, ok, tc.version)
		assert.Equal(t, tc.want, got, tc.version)
	}
}

// TestParseMinor_AllocatesNothing pins what reading a minor costs: Check and
// CheckFormat ask it of every object the load phase judges, each a reference
// reaches included, so the prefix it compares is a slice of the version rather
// than a string built from its parts.
func TestParseMinor_AllocatesNothing(t *testing.T) {
	assert.Zero(t, testing.AllocsPerRun(100, func() { _, _ = specrules.ParseMinor("3.1.0") }))
}

// TestMinor_StringSpellsTheMinor pins the spelling the messages use.
func TestMinor_StringSpellsTheMinor(t *testing.T) {
	assert.Equal(t, "3.0", specrules.V30.String())
	assert.Equal(t, "3.1", specrules.V31.String())
	assert.Equal(t, "3.2", specrules.V32.String())
	assert.Equal(t, "none", specrules.Minor(0).String())
}

// validSpan reports whether minorVersionRange is empty or runs forward over the minors the
// table covers, which every span a row writes must be.
func validSpan(minorVersionRange specrules.MinorVersionRange) bool {
	if minorVersionRange.Since == 0 {
		return minorVersionRange.Until == 0
	}
	last := slices.Max(minors())
	return minorVersionRange.Since <= last && minorVersionRange.Until <= last && (minorVersionRange.Until == 0 || minorVersionRange.Until >= minorVersionRange.Since)
}

// overlaps reports whether the two spans share a minor.
func overlaps(a, b specrules.MinorVersionRange) bool {
	return slices.ContainsFunc(minors(), func(minor specrules.Minor) bool { return a.Holds(minor) && b.Holds(minor) })
}

// TestSpan_HoldsTheMinorsItNames walks every constructor over every minor.
func TestSpan_HoldsTheMinorsItNames(t *testing.T) {
	tests := []struct {
		name string
		span specrules.MinorVersionRange
		want [3]bool // V30, V31, V32
	}{
		{"always", specrules.Always(), [3]bool{true, true, true}},
		{"from 3.1", specrules.From(specrules.V31), [3]bool{false, true, true}},
		{"only 3.1", specrules.Only(specrules.V31), [3]bool{false, true, false}},
		{"through 3.1", specrules.Through(specrules.V31), [3]bool{true, true, false}},
		{"zero", specrules.MinorVersionRange{}, [3]bool{false, false, false}},
	}
	for _, tc := range tests {
		for i, minor := range []specrules.Minor{specrules.V30, specrules.V31, specrules.V32} {
			assert.Equal(t, tc.want[i], tc.span.Holds(minor), "%s at %s", tc.name, minor)
		}
		assert.True(t, validSpan(tc.span), tc.name)
	}
}

// TestSpan_Later pins the question the engine asks of a span.
func TestSpan_Later(t *testing.T) {
	assert.True(t, specrules.From(specrules.V32).Later(specrules.V31), "3.2 is after a 3.1 document")
	assert.False(t, specrules.From(specrules.V31).Later(specrules.V31))
	assert.False(t, specrules.MinorVersionRange{}.Later(specrules.V30), "an empty span is never after anything")
}

// TestValidSpan_RefusesBackwardsAndUnknownRuns pins what the table tests take a
// well-formed span to be, so the check holding every row to it can fail.
func TestValidSpan_RefusesBackwardsAndUnknownRuns(t *testing.T) {
	assert.False(t, validSpan(specrules.MinorVersionRange{Since: specrules.V32, Until: specrules.V30}), "runs backwards")
	assert.False(t, validSpan(specrules.MinorVersionRange{Since: specrules.V30, Until: 9}), "ends at an unknown minor")
	assert.False(t, validSpan(specrules.MinorVersionRange{Since: 9}), "starts at an unknown minor")
	assert.False(t, validSpan(specrules.MinorVersionRange{Until: specrules.V31}), "an end with no start")
}

// TestOverlaps_OnSharedMinors pins when the table tests take two spans to
// conflict.
func TestOverlaps_OnSharedMinors(t *testing.T) {
	assert.True(t, overlaps(specrules.Through(specrules.V31), specrules.From(specrules.V31)))
	assert.False(t, overlaps(specrules.Only(specrules.V30), specrules.From(specrules.V31)))
	assert.False(t, overlaps(specrules.MinorVersionRange{}, specrules.Always()))
}
