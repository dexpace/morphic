package oas_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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
	}
	for _, tc := range tests {
		got, ok := oas.ParseMinor(tc.version)
		assert.Equal(t, tc.ok, ok, tc.version)
		assert.Equal(t, tc.want, got, tc.version)
	}
}

// TestMinor_StringSpellsTheMinor pins the spelling the messages use.
func TestMinor_StringSpellsTheMinor(t *testing.T) {
	assert.Equal(t, "3.0", oas.V30.String())
	assert.Equal(t, "3.1", oas.V31.String())
	assert.Equal(t, "3.2", oas.V32.String())
	assert.Equal(t, "none", oas.Minor(0).String())
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
		{"never", oas.Never(), [3]bool{false, false, false}},
		{"zero", oas.Span{}, [3]bool{false, false, false}},
	}
	for _, tc := range tests {
		for i, m := range []oas.Minor{oas.V30, oas.V31, oas.V32} {
			assert.Equal(t, tc.want[i], tc.span.Holds(m), "%s at %s", tc.name, m)
		}
		assert.True(t, tc.span.Valid(), tc.name)
	}
}

// TestSpan_EmptyAndLater pins the two questions the engine asks of a span.
func TestSpan_EmptyAndLater(t *testing.T) {
	assert.True(t, oas.Never().Empty())
	assert.False(t, oas.Always().Empty())
	assert.True(t, oas.From(oas.V32).Later(oas.V31), "3.2 is after a 3.1 document")
	assert.False(t, oas.From(oas.V31).Later(oas.V31))
	assert.False(t, oas.Never().Later(oas.V30), "an empty span is never after anything")
}

// TestSpan_ValidRefusesBackwardsAndUnknownRuns pins what a well-formed span is.
func TestSpan_ValidRefusesBackwardsAndUnknownRuns(t *testing.T) {
	assert.False(t, oas.Span{Since: oas.V32, Until: oas.V30}.Valid(), "runs backwards")
	assert.False(t, oas.Span{Since: oas.V30, Until: 9}.Valid(), "ends at an unknown minor")
	assert.False(t, oas.Span{Since: 9}.Valid(), "starts at an unknown minor")
	assert.False(t, oas.Span{Until: oas.V31}.Valid(), "an end with no start")
}

// TestSpan_OverlapsOnSharedMinors pins when two spans conflict.
func TestSpan_OverlapsOnSharedMinors(t *testing.T) {
	assert.True(t, oas.Through(oas.V31).Overlaps(oas.From(oas.V31)))
	assert.False(t, oas.Only(oas.V30).Overlaps(oas.From(oas.V31)))
	assert.False(t, oas.Never().Overlaps(oas.Always()))
}

// TestKinds_ListsEveryKindOnce pins the enumeration the table tests lean on.
func TestKinds_ListsEveryKindOnce(t *testing.T) {
	kinds := oas.Kinds()
	require.NotEmpty(t, kinds)
	assert.Equal(t, oas.Document, kinds[0])
	assert.Equal(t, oas.SecurityRequirement, kinds[len(kinds)-1])
	for i := 1; i < len(kinds); i++ {
		assert.Equal(t, kinds[i-1]+1, kinds[i], "the kinds run in sequence")
	}
}
