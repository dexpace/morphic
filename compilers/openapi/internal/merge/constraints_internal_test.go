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
	got := mergeConstraints(
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
		{name: "only one side bounds", keyword: "minimum", a: bigVal("10")},
		{name: "neither side bounds", keyword: "minimum"},
		{
			name:    "the same magnitude, spelled differently",
			keyword: "minimum", a: bigVal("10"), b: bigVal("10.0"),
		},
		{
			name: "differing magnitudes", keyword: "minimum", a: bigVal("10"), b: bigVal("20"),
			want: "conflicting minimum (10 and 20)",
		},
		{
			name:    "the exclusive bound reports under its own keyword",
			keyword: "exclusiveMinimum", a: bigVal("10"), b: bigVal("20"),
			want: "conflicting exclusiveMinimum (10 and 20)",
		},
		{
			name:    "multipleOf shares the comparison",
			keyword: "multipleOf", a: bigVal("3"), b: bigVal("5"),
			want: "conflicting multipleOf (3 and 5)",
		},
		{
			name:    "unparseable operands compare exactly",
			keyword: "minimum", a: bigVal("nan"), b: bigVal("other"),
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
