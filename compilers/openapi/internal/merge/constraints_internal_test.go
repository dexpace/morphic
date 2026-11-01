// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package merge

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// TestMerger_MergeConstraintsKeepsBothSidesBounds covers the merge half rather
// than the conflict half: constraints that do not disagree combine.
func TestMerger_MergeConstraintsKeepsBothSidesBounds(t *testing.T) {
	t.Parallel()
	got := Constraints(
		&ir.Constraints{MaxLength: new(int64(10))},
		&ir.Constraints{MinLength: new(int64(2))},
	)
	require.NotNil(t, got)
	require.NotNil(t, got.MaxLength)
	require.NotNil(t, got.MinLength)
	assert.Equal(t, int64(10), *got.MaxLength)
	assert.Equal(t, int64(2), *got.MinLength, "a bound only the second side declares survives the merge")
}

// TestBigValConflictDetail_ComparesByMagnitude pins the comparison every
// arbitrary-precision keyword goes through. Equal magnitudes spelled
// differently must not read as a conflict — 10 and 10.0 are one value, and
// reporting them would invent a disagreement the source never wrote — while
// differing magnitudes must, since the merge keeps one and drops the other.
//
// The keyword is a parameter, so the name in the message is the one the caller
// passed: the four bounds and multipleOf share this helper, and a hard-coded
// name would report every one of them as the same keyword.
func TestBigValConflictDetail_ComparesByMagnitude(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		keyword string
		a, b    *ir.BigVal
		want    string
	}{
		{name: "only one side bounds", keyword: "minimum", a: new(ir.BigVal("10"))},
		{name: "neither side bounds", keyword: "minimum"},
		{
			name:    "the same magnitude, spelled differently",
			keyword: "minimum", a: new(ir.BigVal("10")), b: new(ir.BigVal("10.0")),
		},
		{
			name: "differing magnitudes", keyword: "minimum", a: new(ir.BigVal("10")), b: new(ir.BigVal("20")),
			want: "conflicting minimum (10 and 20)",
		},
		{
			name:    "the exclusive bound reports under its own keyword",
			keyword: "exclusiveMinimum", a: new(ir.BigVal("10")), b: new(ir.BigVal("20")),
			want: "conflicting exclusiveMinimum (10 and 20)",
		},
		{
			name:    "multipleOf shares the comparison",
			keyword: "multipleOf", a: new(ir.BigVal("3")), b: new(ir.BigVal("5")),
			want: "conflicting multipleOf (3 and 5)",
		},
		{
			name:    "unparseable operands compare exactly",
			keyword: "minimum", a: new(ir.BigVal("nan")), b: new(ir.BigVal("other")),
			want: "conflicting minimum (nan and other)",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			detail, ok := bigValConflictDetail(tc.keyword, tc.a, tc.b)
			assert.Equal(t, tc.want != "", ok)
			assert.Equal(t, tc.want, detail)
		})
	}
}

// TestSameNumber_SeparatesOneValueFromTwo pins what the allOf redeclaration
// merge asks of this comparison: one value written two ways stays one value at
// any magnitude, and two values stay two.
//
// The rows past a rational's range carry the test, since math/big will not
// build 1e1000001. A pair outside the decimal grammar is judged by its text
// alone: identical text is one literal, and differing text is never folded into
// one value. Both directions are asserted, so an answer right one way round is
// not mistaken for a comparison.
func TestSameNumber_SeparatesOneValueFromTwo(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		a, b    ir.BigVal
		outside bool // one of the pair is outside the decimal grammar
		want    bool
	}{
		{name: "one literal against itself", a: "1e1000001", b: "1e1000001", want: true},
		{name: "an exponent against the digits it stands for", a: "1e2", b: "100", want: true},
		{name: "a digit the exponent moved, past a rational", a: "1e1000001", b: "10e1000000", want: true},
		{name: "a magnitude too small for a rational", a: "1e-1000001", b: "10e-1000002", want: true},
		{name: "zero however it is written", a: "0", b: "-0.0", want: true},
		{name: "one exponent apart, past a rational", a: "1e1000001", b: "1e1000002"},
		{name: "one leading digit apart, past a rational", a: "1e1000001", b: "2e1000001"},
		{name: "a sign apart, past a rational", a: "1e1000001", b: "-1e1000001"},
		{name: "a digit past float64's exact range", a: "9007199254740993", b: "9007199254740992"},
		{name: "one unreadable literal against itself", a: "1p4", b: "1p4", outside: true, want: true},
		{name: "an unreadable literal against one that reads", a: "1p4", b: "16", outside: true},
		{name: "two different unreadable literals", a: "1p4", b: "2p1", outside: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, errA := tc.a.Decimal()
			_, errB := tc.b.Decimal()
			require.Equal(t, tc.outside, errA != nil || errB != nil, "the row says whether a literal is outside the grammar")

			assert.Equal(t, tc.want, sameNumber(tc.a, tc.b), "%s against %s", tc.a, tc.b)
			assert.Equal(t, tc.want, sameNumber(tc.b, tc.a), "%s against %s", tc.b, tc.a)
		})
	}
}
